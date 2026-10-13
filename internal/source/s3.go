package source

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
	"github.com/BlueSkyXN/XRAG/adapters"
	"github.com/BlueSkyXN/XRAG/core"
	"github.com/BlueSkyXN/XRAG/internal/store"
)

var readClient = &http.Client{Timeout: 40 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return fmt.Errorf("source redirects are disabled") }}

func readResponse(req *http.Request, max int) ([]byte, http.Header, error) {
	r, e := readClient.Do(req)
	if e != nil {
		return nil, nil, core.Err("SOURCE_UNAVAILABLE", "remote source request failed", 503)
	}
	defer r.Body.Close()
	if r.StatusCode == 412 {
		return nil, nil, core.Err("SOURCE_CHANGED", "object changed during scan", 409)
	}
	if r.StatusCode != 200 {
		return nil, nil, core.Err("SOURCE_UNAVAILABLE", fmt.Sprintf("remote source HTTP %d", r.StatusCode), 503)
	}
	b, e := io.ReadAll(io.LimitReader(r.Body, int64(max)+1))
	if e != nil {
		return nil, nil, e
	}
	if len(b) > max {
		return nil, nil, core.Err("SOURCE_LIMIT", "remote response exceeds bounded size", 422)
	}
	return b, r.Header, nil
}
func awsEncode(v string, slash bool) string {
	var b strings.Builder
	const h = "0123456789ABCDEF"
	for _, x := range []byte(v) {
		if x >= 'a' && x <= 'z' || x >= 'A' && x <= 'Z' || x >= '0' && x <= '9' || strings.ContainsRune("-_.~", rune(x)) || (slash && x == '/') {
			b.WriteByte(x)
		} else {
			b.WriteByte('%')
			b.WriteByte(h[x>>4])
			b.WriteByte(h[x&15])
		}
	}
	return b.String()
}
func canonicalQuery(v url.Values) string {
	a := []string{}
	for k, vs := range v {
		for _, x := range vs {
			a = append(a, awsEncode(k, false)+"="+awsEncode(x, false))
		}
	}
	sort.Strings(a)
	return strings.Join(a, "&")
}
func digest(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func mac(k []byte, s string) []byte {
	h := hmac.New(sha256.New, k)
	h.Write([]byte(s))
	return h.Sum(nil)
}

// SignS3 signs a GET request without normalizing the object-key path.
// Exported to permit replay against the AWS published reference signature.
func SignS3(r *http.Request, access, secret, token, region string, now time.Time) {
	SignS3Payload(r, access, secret, token, region, now, "")
}
func SignS3Payload(r *http.Request, access, secret, token, region string, now time.Time, payload string) {
	now = now.UTC()
	stamp := now.Format("20060102T150405Z")
	day := now.Format("20060102")
	r.Header.Set("X-Amz-Date", stamp)
	r.Header.Set("X-Amz-Content-Sha256", digest(payload))
	if token != "" {
		r.Header.Set("X-Amz-Security-Token", token)
	}
	headers := map[string]string{"host": r.URL.Host}
	for k, v := range r.Header {
		key := strings.ToLower(k)
		if strings.HasPrefix(key, "x-amz-") || key == "if-match" || key == "if-none-match" || key == "range" {
			headers[key] = strings.Join(strings.Fields(strings.Join(v, ",")), " ")
		}
	}
	names := []string{}
	for k := range headers {
		names = append(names, k)
	}
	sort.Strings(names)
	var canon strings.Builder
	for _, k := range names {
		canon.WriteString(k + ":" + headers[k] + "\n")
	}
	signed := strings.Join(names, ";")
	scope := day + "/" + region + "/s3/aws4_request"
	request := r.Method + "\n" + awsEncode(r.URL.Path, true) + "\n" + canonicalQuery(r.URL.Query()) + "\n" + canon.String() + "\n" + signed + "\n" + digest(payload)
	k := mac([]byte("AWS4"+secret), day)
	k = mac(k, region)
	k = mac(k, "s3")
	k = mac(k, "aws4_request")
	sig := hex.EncodeToString(mac(k, "AWS4-HMAC-SHA256\n"+stamp+"\n"+scope+"\n"+digest(request)))
	r.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+access+"/"+scope+", SignedHeaders="+signed+", Signature="+sig)
}

type s3Object struct {
	Key  string
	ETag string
	Size int64
}
type s3Reader struct {
	s                     store.Source
	access, secret, token string
}

func (c s3Reader) get(ctx context.Context, key string, q url.Values, etag string, max int) ([]byte, http.Header, error) {
	u, e := url.Parse(c.s.Root)
	if e != nil {
		return nil, nil, e
	}
	u.Path = "/" + c.s.Remote.Bucket + "/" + key
	u.RawPath = awsEncode(u.Path, true)
	u.RawQuery = canonicalQuery(q)
	r, e := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if e != nil {
		return nil, nil, e
	}
	if etag != "" {
		r.Header.Set("If-Match", etag)
	}
	if !c.s.Remote.Anonymous {
		SignS3(r, c.access, c.secret, c.token, c.s.Remote.Region, time.Now())
	}
	return readResponse(r, max)
}
func (c s3Reader) list(ctx context.Context) ([]s3Object, error) {
	var out []s3Object
	cursor := ""
	seenTokens := map[string]bool{}
	seenKeys := map[string]bool{}
	total := int64(0)
	for page := 0; page < 1000; page++ {
		q := url.Values{"list-type": {"2"}, "max-keys": {"1000"}, "prefix": {c.s.Remote.Prefix}, "encoding-type": {"url"}}
		if cursor != "" {
			q.Set("continuation-token", cursor)
		}
		b, _, e := c.get(ctx, "", q, "", 8<<20)
		if e != nil {
			return nil, e
		}
		var v struct {
			XMLName  xml.Name   `xml:"ListBucketResult"`
			Encoding string     `xml:"EncodingType"`
			More     bool       `xml:"IsTruncated"`
			Next     string     `xml:"NextContinuationToken"`
			Items    []s3Object `xml:"Contents"`
		}
		if e = xml.Unmarshal(b, &v); e != nil {
			return nil, core.Err("SOURCE_INVALID", "invalid S3 inventory XML", 422)
		}
		for _, o := range v.Items {
			if v.Encoding == "url" {
				o.Key, e = url.PathUnescape(o.Key)
				if e != nil {
					return nil, e
				}
			}
			if !strings.HasPrefix(o.Key, c.s.Remote.Prefix) {
				return nil, core.Err("SOURCE_INVALID", "S3 returned key outside prefix", 422)
			}
			rel := strings.TrimPrefix(o.Key, c.s.Remote.Prefix)
			if !supported(rel) {
				continue
			}
			if _, e = core.CleanPath(rel); e != nil {
				return nil, e
			}
			if seenKeys[o.Key] {
				return nil, core.Err("SOURCE_CHANGED", "duplicate key in paginated S3 inventory", 409)
			}
			seenKeys[o.Key] = true
			if o.ETag == "" || o.Size < 0 || o.Size > MaxFileBytes {
				return nil, core.Err("SOURCE_LIMIT", "S3 file has no ETag or is too large", 422)
			}
			total += o.Size
			if total > MaxScanBytes || len(out) >= MaxFiles {
				return nil, core.Err("SOURCE_LIMIT", "S3 inventory exceeds source limits", 422)
			}
			out = append(out, o)
		}
		if !v.More {
			sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
			return out, nil
		}
		if v.Next == "" || seenTokens[v.Next] {
			return nil, core.Err("SOURCE_INVALID", "invalid repeated S3 continuation token", 422)
		}
		seenTokens[v.Next] = true
		cursor = v.Next
	}
	return nil, core.Err("SOURCE_LIMIT", "too many S3 inventory pages", 422)
}
func scanS3(ctx context.Context, s store.Source) (Scan, error) {
	out := Scan{Consistency: "object_conditional_live", Entries: []Entry{}}
	if s.Remote == nil {
		return out, core.Err("INVALID_ARGUMENT", "S3 config missing", 400)
	}
	c := s3Reader{s: s}
	var e error
	if !s.Remote.Anonymous {
		if c.access, e = adapters.Secret(s.Remote.AccessKey); e != nil {
			return out, e
		}
		if c.secret, e = adapters.Secret(s.Remote.SecretKey); e != nil {
			return out, e
		}
		if s.Remote.SessionToken != "" {
			if c.token, e = adapters.Secret(s.Remote.SessionToken); e != nil {
				return out, e
			}
		}
	}
	entries, e := c.list(ctx)
	if e != nil {
		return out, e
	}
	total := 0
	for _, o := range entries {
		b, h, e := c.get(ctx, o.Key, nil, o.ETag, MaxFileBytes)
		if e != nil {
			return out, e
		}
		if len(b) != int(o.Size) || h.Get("ETag") != "" && h.Get("ETag") != o.ETag {
			return out, core.Err("SOURCE_CHANGED", "S3 object size/ETag changed", 409)
		}
		text, e := core.Normalize(b)
		if e != nil {
			return out, e
		}
		total += len(text)
		if total > MaxScanBytes {
			return out, core.Err("SOURCE_LIMIT", "S3 text exceeds limit", 422)
		}
		out.Entries = append(out.Entries, Entry{Path: strings.TrimPrefix(o.Key, s.Remote.Prefix), Body: text, Blob: o.ETag})
	}
	// Re-check the observed inventory; this is not a multi-object atomic snapshot.
	after, e := c.list(ctx)
	if e != nil {
		return out, e
	}
	if core.Hash(after) != core.Hash(entries) {
		return out, core.Err("SOURCE_CHANGED", "S3 inventory changed during reading", 409)
	}
	out.Revision = "inventory:" + core.Hash(entries)
	return out, nil
}
