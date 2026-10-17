package cli

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/BlueSkyXN/XRAG/contracts"
	"github.com/BlueSkyXN/XRAG/core"
)

func imagePayload(p Profile, raw []byte) (map[string]any, error) {
	if p.Type != "server" {
		return nil, core.Err("MODE_UNSUPPORTED", "image describe requires a Server profile", 422)
	}
	var payload map[string]any
	if json.Unmarshal(raw, &payload) != nil || payload == nil {
		return nil, core.Err("INVALID_ARGUMENT", "request must be a JSON object", 400)
	}
	if _, ok := payload["kb"]; !ok {
		if len(p.KBs) != 1 {
			return nil, core.Err("INVALID_ARGUMENT", "select one KB with --kb or the request's kb field", 400)
		}
		payload["kb"] = p.KBs[0]
	}
	spec, _ := contracts.Find("image.describe")
	if e := contracts.Validate(contracts.Input(spec), payload); e != nil {
		return nil, core.Err("INVALID_ARGUMENT", e.Error(), 400)
	}
	prompt := payload["prompt"].(string)
	if strings.TrimSpace(prompt) == "" || len(prompt) > 4000 {
		return nil, core.Err("INVALID_ARGUMENT", "prompt must contain 1..4000 bytes", 400)
	}
	for _, image := range payload["images"].([]any) {
		data, e := base64.StdEncoding.DecodeString(image.(map[string]any)["data_base64"].(string))
		if e != nil || len(data) == 0 {
			return nil, core.Err("INVALID_ARGUMENT", "image is not valid base64 data", 400)
		}
	}
	return payload, nil
}

func describeImage(ctx context.Context, p Profile, opts map[string]any, files string, prompt []string, dry bool, out io.Writer) error {
	if p.Type != "server" {
		return core.Err("MODE_UNSUPPORTED", "image describe requires a Server profile", 422)
	}
	if opts == nil {
		opts = map[string]any{}
	}
	if files != "" {
		images := []map[string]any{}
		for _, name := range strings.Split(files, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			if len(images) == 4 {
				return core.Err("INVALID_ARGUMENT", "image describe accepts at most 4 images", 400)
			}
			mime := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp", ".gif": "image/gif"}[strings.ToLower(filepath.Ext(name))]
			if mime == "" {
				return core.Err("INVALID_ARGUMENT", "image files must use .png, .jpg, .jpeg, .webp or .gif", 400)
			}
			f, e := os.Open(name)
			if e != nil {
				return e
			}
			// The request schema allows 700000 base64 characters per image.
			const maxBytes = 700000 / 4 * 3
			data, e := io.ReadAll(io.LimitReader(f, maxBytes+1))
			f.Close()
			if e != nil {
				return e
			}
			if len(data) > maxBytes {
				return core.Err("SOURCE_LIMIT", filepath.Base(name)+" exceeds 525000 bytes; compress the image before sending", 422)
			}
			images = append(images, map[string]any{"mime": mime, "data_base64": base64.StdEncoding.EncodeToString(data)})
		}
		opts["images"] = images
	}
	if len(prompt) > 0 {
		opts["prompt"] = strings.Join(prompt, " ")
	}
	raw, e := json.Marshal(opts)
	if e != nil {
		return e
	}
	payload, e := imagePayload(p, raw)
	if e != nil {
		return e
	}
	if dry {
		return print(out, map[string]any{"action": "image.describe", "mode": p.Type, "kb": payload["kb"], "images": len(payload["images"].([]any)), "will_execute": false, "endpoint": p.URL})
	}
	v, e := Remote(ctx, p, "POST", "/v1/describe", payload, "")
	if e != nil {
		return e
	}
	return print(out, v)
}
