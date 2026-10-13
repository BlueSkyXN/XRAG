package source

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

func writeS3(ctx context.Context, t store.SyncTarget, rel, body, expected string) (string, error) {
	if t.Remote == nil {
		return "", core.Err("INVALID_ARGUMENT", "S3 destination configuration is absent", 400)
	}
	access, e := adapters.Secret(t.Remote.AccessKey)
	if e != nil {
		return "", e
	}
	secret, e := adapters.Secret(t.Remote.SecretKey)
	if e != nil {
		return "", e
	}
	token, e := adapters.Secret(t.Remote.SessionToken)
	if e != nil {
		return "", e
	}
	u, e := url.Parse(t.Root)
	if e != nil {
		return "", e
	}
	u.Path = "/" + t.Remote.Bucket + "/" + rel
	u.RawPath = awsEncode(u.Path, true)
	get, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return "", e
	}
	SignS3(get, access, secret, token, t.Remote.Region, time.Now())
	res, e := readClient.Do(get)
	if e != nil {
		return "", core.Err("SOURCE_UNAVAILABLE", "S3 destination read failed", 503)
	}
	raw, e := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	res.Body.Close()
	if e != nil {
		return "", e
	}
	etag := res.Header.Get("ETag")
	exists := res.StatusCode == 200
	if exists {
		if len(raw) > 2<<20 {
			return "", core.Err("SYNC_CONFLICT", "destination object too large", 409)
		}
		old, e := core.Normalize(raw)
		if e != nil {
			return "", e
		}
		if core.TextHash(old) == core.TextHash(body) {
			return etag, nil
		}
		if expected == "" || core.TextHash(old) != expected || etag == "" {
			return "", core.Err("SYNC_CONFLICT", "S3 object changed outside XRAG", 409)
		}
	} else if res.StatusCode != 404 {
		return "", core.Err("SOURCE_UNAVAILABLE", "cannot determine S3 object state", 503)
	}
	put, e := http.NewRequestWithContext(ctx, "PUT", u.String(), strings.NewReader(body))
	if e != nil {
		return "", e
	}
	put.Header.Set("Content-Type", "text/markdown; charset=utf-8")
	if exists {
		put.Header.Set("If-Match", etag)
	} else {
		put.Header.Set("If-None-Match", "*")
	}
	SignS3Payload(put, access, secret, token, t.Remote.Region, time.Now(), body)
	res, e = readClient.Do(put)
	if e != nil {
		return "", core.Err("SYNC_OUTCOME_UNKNOWN", "S3 may have accepted the write; retry resolves by content", 503)
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 65536))
	if res.StatusCode == 409 || res.StatusCode == 412 {
		return "", core.Err("SYNC_CONFLICT", "conditional S3 write was rejected", 409)
	}
	if res.StatusCode != 200 && res.StatusCode != 201 {
		return "", core.Err("SOURCE_UNAVAILABLE", "S3 write failed", 503)
	}
	version := res.Header.Get("X-Amz-Version-Id")
	if version == "" {
		version = res.Header.Get("ETag")
	}
	if version == "" {
		version = core.TextHash(body)
	}
	return version, nil
}
