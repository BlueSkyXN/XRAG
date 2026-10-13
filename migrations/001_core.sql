-- Applied by the M1 server as the V0.8 key-table contract.
-- M1 writes file, build, lexical and query tables only.
-- XRAG V0.8 key-table DESIGN contract. Not a released application migration.
PRAGMA foreign_keys=ON;
PRAGMA busy_timeout=5000;
CREATE TABLE kb (
 id TEXT PRIMARY KEY NOT NULL, name TEXT NOT NULL, config_rev INTEGER NOT NULL DEFAULT 1,
 revoke_epoch INTEGER NOT NULL DEFAULT 0, dependency_epoch INTEGER NOT NULL DEFAULT 0, active_generation TEXT,
 config_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(config_json))
);
CREATE TABLE source (
 id TEXT PRIMARY KEY NOT NULL, kb_id TEXT NOT NULL REFERENCES kb(id),
 kind TEXT NOT NULL CHECK(kind IN ('path','git','oss','builtin','weknora')),
 mount TEXT NOT NULL, config_rev INTEGER NOT NULL DEFAULT 1,
 config_json TEXT NOT NULL CHECK(json_valid(config_json)), watermark TEXT,
 UNIQUE(kb_id,mount)
);
CREATE TABLE build (
 id TEXT PRIMARY KEY NOT NULL, kb_id TEXT NOT NULL REFERENCES kb(id),
 mode TEXT NOT NULL CHECK(mode IN ('incremental','full','vectors_only','graph_only')),
 state TEXT NOT NULL, started_at INTEGER, ended_at INTEGER
);
CREATE TABLE knowledge_item (
 id TEXT PRIMARY KEY NOT NULL, kb_id TEXT NOT NULL REFERENCES kb(id), path TEXT NOT NULL,
 kind TEXT NOT NULL CHECK(kind IN ('case','fact','rule','procedure','wiki')),
 status TEXT NOT NULL CHECK(status IN ('draft','published','needs_review','blocked','archived','withdrawn')),
 draft_revision TEXT, published_revision TEXT,
 UNIQUE(kb_id,path), UNIQUE(id,kb_id)
);
CREATE TABLE knowledge_revision (
 id TEXT PRIMARY KEY NOT NULL, item_id TEXT NOT NULL, kb_id TEXT NOT NULL,
 content_hash TEXT NOT NULL, content_path TEXT NOT NULL,
 evidence_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(evidence_json)),
 review_state TEXT NOT NULL DEFAULT 'pending' CHECK(review_state IN ('pending','approved','rejected')),
 dependency_state TEXT NOT NULL DEFAULT 'unknown' CHECK(dependency_state IN ('valid','needs_review','blocked','unknown')),
 checked_dependency_epoch INTEGER NOT NULL DEFAULT -1,
 created_at INTEGER NOT NULL, UNIQUE(id,kb_id), UNIQUE(id,item_id,kb_id),
 FOREIGN KEY(item_id,kb_id) REFERENCES knowledge_item(id,kb_id)
);
CREATE TABLE file (
 kb_id TEXT NOT NULL REFERENCES kb(id), path TEXT NOT NULL,
 source_id TEXT NOT NULL REFERENCES source(id), active_edition TEXT,
 withdrawn INTEGER NOT NULL DEFAULT 0 CHECK(withdrawn IN (0,1)),
 knowledge_item_id TEXT REFERENCES knowledge_item(id),
 PRIMARY KEY(kb_id,path)
);
CREATE TABLE file_edition (
 id TEXT PRIMARY KEY NOT NULL, kb_id TEXT NOT NULL, path TEXT NOT NULL,
 file_hash TEXT NOT NULL, settings_hash TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('staging','active','retired','aborted')),
 knowledge_revision_id TEXT REFERENCES knowledge_revision(id),
 content_path TEXT NOT NULL, created_at INTEGER NOT NULL,
 vector_state TEXT NOT NULL DEFAULT 'disabled', graph_state TEXT NOT NULL DEFAULT 'disabled',
 FOREIGN KEY(kb_id,path) REFERENCES file(kb_id,path)
);
-- The active pointer is updated through ActivateFile, never by a query endpoint.
CREATE UNIQUE INDEX one_active_edition ON file_edition(kb_id,path) WHERE state='active';
CREATE TABLE chunk (
 id TEXT PRIMARY KEY NOT NULL, edition_id TEXT NOT NULL REFERENCES file_edition(id),
 chunk_key TEXT NOT NULL, ordinal INTEGER NOT NULL,
 title TEXT NOT NULL, heading_json TEXT NOT NULL CHECK(json_valid(heading_json)), text TEXT NOT NULL,
 char_start INTEGER NOT NULL, char_end INTEGER NOT NULL, line_start INTEGER NOT NULL,
 line_end INTEGER NOT NULL, segments_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(segments_json)),
 CHECK(char_end>=char_start), CHECK(line_end>=line_start),
 UNIQUE(edition_id,chunk_key), UNIQUE(edition_id,ordinal)
);
CREATE VIRTUAL TABLE chunk_fts USING fts5(chunk_id UNINDEXED, title_tokens, body_tokens, tokenize='unicode61');
CREATE TABLE file_field (
 edition_id TEXT NOT NULL REFERENCES file_edition(id), name TEXT NOT NULL,
 value_type TEXT NOT NULL CHECK(value_type IN ('text','number','time')),
 value_key TEXT NOT NULL, value_text TEXT, value_num REAL, value_time TEXT,
 PRIMARY KEY(edition_id,name,value_key),
 CHECK((value_type='text' AND value_text IS NOT NULL AND value_num IS NULL AND value_time IS NULL)
    OR (value_type='number' AND value_num IS NOT NULL AND value_text IS NULL AND value_time IS NULL)
    OR (value_type='time' AND value_time IS NOT NULL AND value_text IS NULL AND value_num IS NULL))
) WITHOUT ROWID;
CREATE TABLE vector_descriptor (
 kb_id TEXT NOT NULL REFERENCES kb(id), generation TEXT NOT NULL,
 physical_collection TEXT NOT NULL UNIQUE, descriptor_hash TEXT NOT NULL,
 descriptor_json TEXT NOT NULL CHECK(json_valid(descriptor_json)),
 ready INTEGER NOT NULL DEFAULT 0 CHECK(ready IN (0,1)), retired_at INTEGER,
 PRIMARY KEY(kb_id,generation)
);
CREATE TABLE processing_cache (
 kind TEXT NOT NULL CHECK(kind IN ('embedding','extraction')),
 cache_key TEXT NOT NULL, input_hash TEXT NOT NULL, profile_hash TEXT NOT NULL,
 result_json TEXT NOT NULL CHECK(json_valid(result_json)), PRIMARY KEY(kind,cache_key)
) WITHOUT ROWID;
CREATE TABLE build_file (
 build_id TEXT NOT NULL REFERENCES build(id), path TEXT NOT NULL,
 action TEXT NOT NULL, state TEXT NOT NULL,
 before_edition TEXT, after_edition TEXT, before_hash TEXT, after_hash TEXT,
 error_json TEXT CHECK(error_json IS NULL OR json_valid(error_json)), PRIMARY KEY(build_id,path)
);
CREATE TABLE scan_entry (
 scan_id TEXT NOT NULL, source_id TEXT NOT NULL REFERENCES source(id), source_revision TEXT,
 path TEXT NOT NULL, content_hint TEXT, fetched_hash TEXT, PRIMARY KEY(scan_id,path)
);
CREATE TABLE job (
 id TEXT PRIMARY KEY NOT NULL, kb_id TEXT REFERENCES kb(id), kind TEXT NOT NULL,
 dedupe_key TEXT NOT NULL, request_hash TEXT NOT NULL,
 state TEXT NOT NULL CHECK(state IN ('queued','running','succeeded','partial','failed','canceled','expired')),
 created_at INTEGER NOT NULL, queue_expires_at INTEGER NOT NULL,
 execution_started_at INTEGER, execution_deadline_at INTEGER,
 lease_token TEXT, lease_until INTEGER, cancel_requested INTEGER NOT NULL DEFAULT 0,
 payload_json TEXT NOT NULL DEFAULT '{}' CHECK(json_valid(payload_json)), result_json TEXT, UNIQUE(id,kb_id)
);
CREATE UNIQUE INDEX one_queued_build ON job(kb_id) WHERE kind='build' AND state='queued';
CREATE UNIQUE INDEX one_running_build ON job(kb_id) WHERE kind='build' AND state='running';
CREATE TABLE idempotency (
 principal_id TEXT NOT NULL, operation TEXT NOT NULL, client_key TEXT NOT NULL,
 request_hash TEXT NOT NULL, job_id TEXT REFERENCES job(id), expires_at INTEGER NOT NULL,
 PRIMARY KEY(principal_id,operation,client_key)
);
CREATE TABLE job_attempt (
 job_id TEXT NOT NULL REFERENCES job(id), attempt INTEGER NOT NULL, lease_token TEXT NOT NULL,
 started_at INTEGER NOT NULL, ended_at INTEGER, reason TEXT,
 PRIMARY KEY(job_id,attempt), UNIQUE(lease_token)
);
CREATE TABLE usage_call (
 call_id TEXT PRIMARY KEY NOT NULL, job_id TEXT REFERENCES job(id), attempt INTEGER NOT NULL,
 provider_kind TEXT NOT NULL, state TEXT NOT NULL CHECK(state IN ('reserved','sent','succeeded','failed','outcome_unknown')),
 reserved_units INTEGER NOT NULL DEFAULT 0, known_units INTEGER NOT NULL DEFAULT 0,
 estimated INTEGER NOT NULL DEFAULT 0 CHECK(estimated IN (0,1)), sent_at INTEGER, ended_at INTEGER,
 CHECK(reserved_units>=0 AND known_units>=0)
);
CREATE TABLE query_view (
 id TEXT PRIMARY KEY NOT NULL, origin_id TEXT NOT NULL, selection_json TEXT NOT NULL CHECK(json_valid(selection_json)),
 descriptors_json TEXT NOT NULL CHECK(json_valid(descriptors_json)), revoke_epochs_json TEXT NOT NULL,
 created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
CREATE TABLE result_set (
 id TEXT PRIMARY KEY NOT NULL, view_id TEXT REFERENCES query_view(id), action TEXT NOT NULL,
 items_json TEXT NOT NULL CHECK(json_valid(items_json)), total_json TEXT NOT NULL CHECK(json_valid(total_json)),
 cache_complete INTEGER NOT NULL CHECK(cache_complete IN (0,1)), expires_at INTEGER NOT NULL
);
CREATE TABLE handle (
 id TEXT PRIMARY KEY NOT NULL, view_id TEXT REFERENCES query_view(id), kind TEXT NOT NULL,
 basis TEXT NOT NULL CHECK(basis IN ('enumerated','candidate','expanded')),
 members_json TEXT NOT NULL CHECK(json_valid(members_json)), origin_scope_json TEXT NOT NULL CHECK(json_valid(origin_scope_json)),
 complete INTEGER NOT NULL CHECK(complete IN (0,1)), created_at INTEGER NOT NULL, expires_at INTEGER NOT NULL
);
CREATE TABLE edition_pin (
 owner_kind TEXT NOT NULL CHECK(owner_kind IN ('view','handle','task')),
 owner_id TEXT NOT NULL, edition_id TEXT NOT NULL REFERENCES file_edition(id),
 expires_at INTEGER NOT NULL, PRIMARY KEY(owner_kind,owner_id,edition_id)
);
CREATE TABLE citation_archive (
 ref_id TEXT PRIMARY KEY NOT NULL, origin_id TEXT NOT NULL, kb_id TEXT NOT NULL REFERENCES kb(id),
 kind TEXT NOT NULL CHECK(kind IN ('chunk','path','aggregate','record')),
 payload_json TEXT NOT NULL CHECK(json_valid(payload_json)), dependencies_json TEXT NOT NULL CHECK(json_valid(dependencies_json)),
 availability TEXT NOT NULL DEFAULT 'available' CHECK(availability IN ('available','withdrawn','expired')),
 created_at INTEGER NOT NULL
);
CREATE TABLE citation_owner (
 ref_id TEXT NOT NULL REFERENCES citation_archive(ref_id), owner_kind TEXT NOT NULL,
 owner_id TEXT NOT NULL, PRIMARY KEY(ref_id,owner_kind,owner_id)
) WITHOUT ROWID;

-- V0.8 additional key contracts. Per-action index coverage is derived from these
-- edition totals plus the per-chunk plan, never copied blindly to a sub-scope.
CREATE TABLE projection_status (
 edition_id TEXT NOT NULL REFERENCES file_edition(id),
 projection TEXT NOT NULL CHECK(projection IN ('lexical','vector','graph')),
 state TEXT NOT NULL CHECK(state IN ('complete','partial')),
 total INTEGER NOT NULL CHECK(total>=0), ready INTEGER NOT NULL CHECK(ready>=0),
 failed INTEGER NOT NULL CHECK(failed>=0), pending INTEGER NOT NULL CHECK(pending>=0),
 excluded INTEGER NOT NULL CHECK(excluded>=0), observed_at INTEGER NOT NULL,
 CHECK(total=ready+failed+pending+excluded),
 CHECK((state='complete' AND ready=total) OR (state='partial' AND ready<total)),
 PRIMARY KEY(edition_id,projection)
);
-- The service validates auto_kinds values and uniqueness using kb.schema.json.
CREATE TABLE kb_publication_policy (
 kb_id TEXT PRIMARY KEY NOT NULL REFERENCES kb(id),
 mode TEXT NOT NULL DEFAULT 'manual' CHECK(mode IN ('manual','auto')),
 auto_kinds_json TEXT NOT NULL DEFAULT '[]' CHECK(json_valid(auto_kinds_json) AND json_type(auto_kinds_json)='array'),
 revision INTEGER NOT NULL DEFAULT 1 CHECK(revision>=1),
 CHECK((mode='manual' AND json_array_length(auto_kinds_json)=0) OR
       (mode='auto' AND json_array_length(auto_kinds_json)>0))
);
CREATE TABLE publication_request (
 id TEXT PRIMARY KEY NOT NULL, kb_id TEXT NOT NULL, item_id TEXT NOT NULL,
 revision_id TEXT NOT NULL, job_id TEXT NOT NULL UNIQUE,
 trigger TEXT NOT NULL CHECK(trigger IN ('manual','policy')), actor_id TEXT NOT NULL,
 policy_revision INTEGER, expected_published_revision TEXT, expected_dependency_epoch INTEGER NOT NULL,
 FOREIGN KEY(item_id,kb_id) REFERENCES knowledge_item(id,kb_id),
 FOREIGN KEY(revision_id,item_id,kb_id) REFERENCES knowledge_revision(id,item_id,kb_id),
 FOREIGN KEY(job_id,kb_id) REFERENCES job(id,kb_id),
 CHECK(trigger='manual' OR policy_revision IS NOT NULL)
);
CREATE TABLE knowledge_dependency (
 owner_revision TEXT NOT NULL, kb_id TEXT NOT NULL,
 target_kind TEXT NOT NULL CHECK(target_kind IN ('file','knowledge')),
 target_key TEXT NOT NULL, target_version TEXT NOT NULL,
 direct INTEGER NOT NULL CHECK(direct IN (0,1)), content_hash TEXT,
 PRIMARY KEY(owner_revision,target_kind,target_key,target_version),
 FOREIGN KEY(owner_revision,kb_id) REFERENCES knowledge_revision(id,kb_id)
) WITHOUT ROWID;
-- target references are resolved within kb_id by DependencyService, including
-- intermediate knowledge revisions. No global reverse dependency index.
CREATE TABLE materialization_journal (
 edition_id TEXT NOT NULL REFERENCES file_edition(id),
 projection TEXT NOT NULL CHECK(projection IN ('lexical','vector','graph')),
 phase TEXT NOT NULL CHECK(phase IN ('prepared','written','verified','cleanup_pending','cleaned')),
 plan_json TEXT NOT NULL CHECK(json_valid(plan_json)), lease_token TEXT,
 updated_at INTEGER NOT NULL, PRIMARY KEY(edition_id,projection)
);

CREATE VIEW visible_chunk AS
 SELECT c.*, e.kb_id, e.path, e.file_hash FROM chunk c
 JOIN file_edition e ON e.id=c.edition_id
 JOIN file f ON f.kb_id=e.kb_id AND f.path=e.path
 LEFT JOIN knowledge_item k ON k.id=f.knowledge_item_id
 WHERE f.withdrawn=0 AND f.active_edition=e.id AND e.state='active'
 AND (f.knowledge_item_id IS NULL OR
      (k.status IN ('published','needs_review') AND k.published_revision=e.knowledge_revision_id));
-- QueryView selects explicit editions; historical reads do not use visible_chunk.
-- Such reads must separately enforce current withdrawn/revoke state and valid pins.

-- visible_chunk is only a base view; service paths must check current dependency
-- epochs for current, cached and historical reads. No SQL claim of transitive revocation.
