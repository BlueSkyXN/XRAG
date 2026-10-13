package adapters

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
)

type Neo4j struct {
	Config NeoConfig
	Origin string
}

func (n *Neo4j) query(ctx context.Context, statement string, parameters any) ([][]json.RawMessage, error) {
	pass, e := Secret(n.Config.Password)
	if e != nil {
		return nil, e
	}
	headers := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(n.Config.User+":"+pass))}
	var res struct {
		Data struct {
			Values [][]json.RawMessage `json:"values"`
		} `json:"data"`
		Errors []map[string]any `json:"errors"`
	}
	e = Call(ctx, "POST", strings.TrimRight(n.Config.URL, "/")+"/db/"+n.Config.Database+"/query/v2", headers, map[string]any{"statement": statement, "parameters": parameters}, &res)
	if e != nil {
		return nil, e
	}
	if len(res.Errors) > 0 {
		return nil, core.Err("BACKEND_UNAVAILABLE", "Neo4j query failed: "+core.ValueString(res.Errors[0]["code"]), 503)
	}
	return res.Data.Values, nil
}
func (n *Neo4j) Write(ctx context.Context, d core.Document) error {
	chunks := []map[string]any{}
	for _, c := range d.Chunks {
		chunks = append(chunks, map[string]any{"id": c.ID, "kb": c.KB, "path": c.Path, "edition": c.Edition, "file_hash": c.FileHash, "title": c.Title, "heading_path": c.Heading, "generation": c.Generation})
	}
	if _, e := n.query(ctx, `UNWIND $rows AS r MERGE (c:XragDemoChunk {authority:$authority,chunk_id:r.id}) SET c.kb=r.kb,c.path=r.path,c.edition_id=r.edition,c.file_hash=r.file_hash,c.title=r.title,c.heading_path=r.heading_path,c.generation=r.generation`, map[string]any{"authority": n.Origin, "rows": chunks}); e != nil {
		return e
	}
	rows := []map[string]any{}
	for _, s := range d.Supports {
		rows = append(rows, map[string]any{"id": s.ID, "from_id": core.EntityID(s.KB, s.From), "to_id": core.EntityID(s.KB, s.To), "from": s.From, "to": s.To, "kb": s.KB, "path": s.Path, "edition": s.Edition, "chunk": s.ChunkID, "rel": s.Relation, "origin": s.Origin})
	}
	// No chunk text or evidence text is stored in Neo4j; provenance remains a chunk identifier.
	_, e := n.query(ctx, `UNWIND $rows AS r MERGE (a:XragDemoEntity {authority:$authority, entity_id:r.from_id}) SET a.name=r.from,a.kb=r.kb MERGE (b:XragDemoEntity {authority:$authority,entity_id:r.to_id}) SET b.name=r.to,b.kb=r.kb MERGE (c:XragDemoChunk {authority:$authority,chunk_id:r.chunk}) SET c.edition_id=r.edition,c.kb=r.kb,c.path=r.path MERGE (s:XragDemoSupport {authority:$authority,support_id:r.id}) SET s.kb=r.kb,s.path=r.path,s.edition_id=r.edition,s.chunk_id=r.chunk,s.rel=r.rel,s.origin=r.origin MERGE (a)-[:SUBJECT_OF]->(s) MERGE (s)-[:OBJECT]->(b) MERGE (c)-[:SUPPORTS]->(s)`, map[string]any{"rows": rows, "authority": n.Origin})
	return e
}
func (n *Neo4j) Supports(ctx context.Context, v core.View) ([]core.Support, error) {
	editions := []string{}
	for _, d := range v.Documents {
		editions = append(editions, d.Edition)
	}
	out := []core.Support{}
	for i := 0; i < len(editions); i += 500 {
		end := i + 500
		if end > len(editions) {
			end = len(editions)
		}
		vals, e := n.query(ctx, `MATCH (a:XragDemoEntity)-[:SUBJECT_OF]->(s:XragDemoSupport)-[:OBJECT]->(b:XragDemoEntity) WHERE s.authority=$authority AND s.edition_id IN $editions RETURN {support_id:s.support_id,kb:s.kb,path:s.path,edition_id:s.edition_id,chunk_id:s.chunk_id,from:a.name,to:b.name,relation:s.rel,origin:s.origin} AS support ORDER BY s.support_id LIMIT 20001`, map[string]any{"authority": v.Origin, "editions": editions[i:end]})
		if e != nil {
			return nil, e
		}
		if len(vals) > 20000 {
			return nil, core.Err("GRAPH_BUDGET_EXCEEDED", "support limit reached", 422)
		}
		for _, row := range vals {
			if len(row) != 1 {
				return nil, core.Err("BACKEND_UNAVAILABLE", "invalid graph response", 503)
			}
			var s core.Support
			if e = json.Unmarshal(row[0], &s); e != nil {
				return nil, e
			}
			out = append(out, s)
			if len(out) > 20000 {
				return nil, core.Err("GRAPH_BUDGET_EXCEEDED", "total raw support limit reached across batches", 422)
			}
		}
	}
	return out, nil
}

// Read an explicitly selected authority/KB as metadata only. Text is never stored in this graph.
func (n *Neo4j) ReadChunks(ctx context.Context, kb string) (string, []core.Chunk, error) {
	authority := n.Origin
	if authority == "" {
		values, err := n.query(ctx, `MATCH (c:XragDemoChunk) WHERE c.kb=$kb RETURN DISTINCT c.authority ORDER BY c.authority LIMIT 2`, map[string]any{"kb": kb})
		if err != nil {
			return "", nil, err
		}
		if len(values) != 1 || len(values[0]) != 1 {
			return "", nil, core.Err("REF_ORIGIN_MISMATCH", "set origin_id: graph contains zero or multiple knowledge authorities", 409)
		}
		if err = json.Unmarshal(values[0][0], &authority); err != nil {
			return "", nil, err
		}
	}
	values, err := n.query(ctx, `MATCH (c:XragDemoChunk) WHERE c.kb=$kb AND c.authority=$authority RETURN {chunk_id:c.chunk_id,edition_id:c.edition_id,kb:c.kb,path:c.path,file_hash:c.file_hash,title:c.title,heading_path:c.heading_path,generation:c.generation} ORDER BY c.chunk_id LIMIT 10001`, map[string]any{"kb": kb, "authority": authority})
	if err != nil {
		return "", nil, err
	}
	if len(values) > 10000 {
		return "", nil, core.Err("RESULT_TOO_LARGE", "graph metadata exceeds direct read limit; use Server query", 422)
	}
	out := []core.Chunk{}
	for _, row := range values {
		if len(row) != 1 {
			return "", nil, core.Err("BACKEND_UNAVAILABLE", "invalid graph chunk response", 503)
		}
		var c core.Chunk
		if err = json.Unmarshal(row[0], &c); err != nil {
			return "", nil, err
		}
		c.TextMissing = true
		out = append(out, c)
	}
	return authority, out, nil
}
func (n *Neo4j) DeleteEdition(ctx context.Context, edition string) error {
	_, e := n.query(ctx, `MATCH (s:XragDemoSupport {authority:$authority,edition_id:$edition}) DETACH DELETE s`, map[string]any{"authority": n.Origin, "edition": edition})
	if e != nil {
		return e
	}
	_, e = n.query(ctx, `MATCH (c:XragDemoChunk {authority:$authority,edition_id:$edition}) DETACH DELETE c`, map[string]any{"authority": n.Origin, "edition": edition})
	if e != nil {
		return e
	}
	_, e = n.query(ctx, `MATCH (e:XragDemoEntity {authority:$authority}) WHERE NOT (e)--() DELETE e`, map[string]any{"authority": n.Origin})
	return e
}

// ChunksByID performs bounded identity reads instead of enumerating a KB.
func (n *Neo4j) ChunksByID(ctx context.Context, kb string, ids []string) ([]core.Chunk, error) {
	out := []core.Chunk{}
	for start := 0; start < len(ids); start += 256 {
		end := min(start+256, len(ids))
		values, e := n.query(ctx, `MATCH (c:XragDemoChunk) WHERE c.authority=$authority AND c.kb=$kb AND c.chunk_id IN $ids RETURN {chunk_id:c.chunk_id,edition_id:c.edition_id,kb:c.kb,path:c.path,file_hash:c.file_hash,title:c.title,heading_path:c.heading_path,generation:c.generation} ORDER BY c.chunk_id`, map[string]any{"authority": n.Origin, "kb": kb, "ids": ids[start:end]})
		if e != nil {
			return nil, e
		}
		if len(values) > end-start {
			return nil, core.Err("BACKEND_UNAVAILABLE", "duplicate graph identities", 503)
		}
		allowed := map[string]bool{}
		for _, id := range ids[start:end] {
			allowed[id] = true
		}
		for _, row := range values {
			if len(row) != 1 {
				return nil, core.Err("BACKEND_UNAVAILABLE", "invalid graph row", 503)
			}
			var c core.Chunk
			if e = json.Unmarshal(row[0], &c); e != nil {
				return nil, e
			}
			if !allowed[c.ID] || c.KB != kb || c.Edition == "" {
				return nil, core.Err("REF_IDENTITY_MISMATCH", "unexpected graph identity", 409)
			}
			c.TextMissing = true
			c.MentionNames = []string{}
			out = append(out, c)
		}
	}
	return out, nil
}

// Frontier reads only adjacency whose supporting chunk is in the explicit
// caller range. Filtering happens in Cypher before LIMIT, not after traversal.
func (n *Neo4j) Frontier(ctx context.Context, v core.View, front []string, dir string, remaining int) ([]core.Support, error) {
	ids := []string{}
	for _, d := range v.Documents {
		for _, c := range d.Chunks {
			ids = append(ids, c.ID)
		}
	}
	ids = core.Unique(ids)
	clause := "(a.entity_id IN $front OR b.entity_id IN $front)"
	if dir == "out" {
		clause = "a.entity_id IN $front"
	}
	if dir == "in" {
		clause = "b.entity_id IN $front"
	}
	out := []core.Support{}
	seen := map[string]bool{}
	for i := 0; i < len(ids); i += 500 {
		statement := `MATCH (a:XragDemoEntity)-[:SUBJECT_OF]->(s:XragDemoSupport)-[:OBJECT]->(b:XragDemoEntity) WHERE s.authority=$authority AND s.chunk_id IN $chunks AND ` + clause + ` RETURN {support_id:s.support_id,kb:s.kb,path:s.path,edition_id:s.edition_id,chunk_id:s.chunk_id,from:a.name,to:b.name,relation:s.rel,origin:s.origin} ORDER BY s.support_id LIMIT $limit`
		vals, err := n.query(ctx, statement, map[string]any{"authority": v.Origin, "chunks": ids[i:min(i+500, len(ids))], "front": front, "limit": remaining + 1})
		if err != nil {
			return nil, err
		}
		for _, row := range vals {
			if len(row) != 1 {
				return nil, core.Err("BACKEND_UNAVAILABLE", "invalid graph row", 503)
			}
			var s core.Support
			if err = json.Unmarshal(row[0], &s); err != nil {
				return nil, err
			}
			if !seen[s.ID] {
				out = append(out, s)
				seen[s.ID] = true
				if len(out) > remaining {
					return nil, core.Err("GRAPH_BUDGET_EXCEEDED", "frontier support budget", 422)
				}
			}
		}
	}
	return out, nil
}

func (n *Neo4j) VerifyRecoveryTarget(ctx context.Context, resume bool) error {
	rows, err := n.query(ctx, `MATCH (n) WHERE n.authority=$authority RETURN count(n)`, map[string]any{"authority": n.Origin})
	if err != nil {
		return err
	}
	if len(rows) != 1 || len(rows[0]) != 1 {
		return core.Err("BACKEND_UNAVAILABLE", "invalid recovery count", 503)
	}
	var count int64
	if err = json.Unmarshal(rows[0][0], &count); err != nil {
		return err
	}
	if count > 0 && !resume {
		return core.Err("RECOVERY_TARGET_NOT_EMPTY", "target graph already contains this authority", 409)
	}
	return nil
}

func (n *Neo4j) ResolveEntity(ctx context.Context, kb, x string) (string, error) {
	rows, err := n.query(ctx, `MATCH (e:XragDemoEntity) WHERE e.authority=$authority AND e.kb=$kb AND (e.entity_id=$identity OR e.name=$identity) RETURN e.entity_id ORDER BY e.entity_id LIMIT 2`, map[string]any{"authority": n.Origin, "kb": kb, "identity": x})
	if err != nil {
		return "", err
	}
	if len(rows) > 1 {
		return "", core.Err("AMBIGUOUS_ENTITY", "use entity_id", 409)
	}
	if len(rows) == 0 {
		return "", nil
	}
	if len(rows[0]) != 1 {
		return "", core.Err("BACKEND_UNAVAILABLE", "invalid entity identity", 503)
	}
	var id string
	err = json.Unmarshal(rows[0][0], &id)
	return id, err
}
