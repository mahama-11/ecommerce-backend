package templatecenter

import (
	"fmt"
	"io"
)

func (s *Service) DownloadExampleAsset(storageKey string) (io.ReadCloser, map[string]string, error) {
	if s.platform == nil {
		return nil, nil, fmt.Errorf("platform client is not configured")
	}
	body, header, err := s.platform.DownloadAsset(storageKey)
	if err != nil {
		return nil, nil, err
	}
	out := map[string]string{}
	if contentType := header.Get("Content-Type"); contentType != "" {
		out["Content-Type"] = contentType
	}
	if cacheControl := header.Get("Cache-Control"); cacheControl != "" {
		out["Cache-Control"] = cacheControl
	}
	if contentLength := header.Get("Content-Length"); contentLength != "" {
		out["Content-Length"] = contentLength
	}
	return body, out, nil
}
