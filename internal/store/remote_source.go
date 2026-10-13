package store

import (
	"net/url"
	"regexp"
	"strings"
	"github.com/BlueSkyXN/XRAG/core"
)

// RemoteSource is immutable for a registered source; credentials are references,
// not values. A changed endpoint or upstream KB requires a new source binding.
type RemoteSource struct {
	Region        string `json:"region,omitempty"`
	Bucket        string `json:"bucket,omitempty"`
	Prefix        string `json:"prefix,omitempty"`
	AccessKey     string `json:"access_key,omitempty"`
	SecretKey     string `json:"secret_key,omitempty"`
	SessionToken  string `json:"session_token,omitempty"`
	Anonymous     bool   `json:"anonymous,omitempty"`
	KnowledgeBase string `json:"knowledge_base,omitempty"`
	APIKey        string `json:"api_key,omitempty"`
	AuthHeader    string `json:"auth_header,omitempty"`
}

func validSecretRef(v string) bool {
	return (strings.HasPrefix(v, "env:") && len(v) > 4) || (strings.HasPrefix(v, "file:") && len(v) > 5)
}
func validateRemoteSource(s *Source) error {
	u, e := url.Parse(s.Root)
	if e != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		return core.Err("INVALID_ARGUMENT", "remote source root must be an HTTP(S) origin without credentials, query or path", 400)
	}
	s.Root = strings.TrimRight(s.Root, "/")
	if s.Remote == nil {
		return core.Err("INVALID_ARGUMENT", "remote source configuration required", 400)
	}
	c := s.Remote
	if s.Kind == "s3" {
		if !regexp.MustCompile(`^[a-z0-9][a-z0-9.-]{1,61}[a-z0-9]$`).MatchString(c.Bucket) || strings.Contains(c.Bucket, "..") {
			return core.Err("INVALID_ARGUMENT", "invalid S3 bucket", 400)
		}
		if c.Region == "" {
			c.Region = "us-east-1"
		}
		if !regexp.MustCompile(`^[a-z0-9-]{1,64}$`).MatchString(c.Region) {
			return core.Err("INVALID_ARGUMENT", "invalid S3 region", 400)
		}
		if c.Prefix != "" {
			p := strings.TrimSuffix(c.Prefix, "/")
			if _, e := core.CleanPath(p); e != nil {
				return e
			}
			c.Prefix = p + "/"
		}
		if !c.Anonymous && (!validSecretRef(c.AccessKey) || !validSecretRef(c.SecretKey)) {
			return core.Err("INVALID_ARGUMENT", "S3 access_key and secret_key require env/file references", 400)
		}
		if c.Anonymous && (c.AccessKey != "" || c.SecretKey != "" || c.SessionToken != "") {
			return core.Err("INVALID_ARGUMENT", "anonymous S3 may not include credentials", 400)
		}
		if c.SessionToken != "" && !validSecretRef(c.SessionToken) {
			return core.Err("INVALID_ARGUMENT", "session_token requires env/file reference", 400)
		}
		if c.KnowledgeBase != "" || c.APIKey != "" || c.AuthHeader != "" {
			return core.Err("INVALID_ARGUMENT", "WeKnora options do not apply to S3", 400)
		}
	} else {
		if !regexp.MustCompile(`^[A-Za-z0-9_-]{1,128}$`).MatchString(c.KnowledgeBase) || !validSecretRef(c.APIKey) {
			return core.Err("INVALID_ARGUMENT", "WeKnora knowledge_base and env/file api_key required", 400)
		}
		if c.AuthHeader == "" {
			c.AuthHeader = "X-API-Key"
		}
		if c.AuthHeader != "X-API-Key" && c.AuthHeader != "Authorization" {
			return core.Err("INVALID_ARGUMENT", "auth_header must be X-API-Key or Authorization", 400)
		}
		if c.Bucket != "" || c.Prefix != "" || c.Region != "" || c.AccessKey != "" || c.SecretKey != "" || c.SessionToken != "" || c.Anonymous {
			return core.Err("INVALID_ARGUMENT", "S3 options do not apply to WeKnora", 400)
		}
	}
	return nil
}
