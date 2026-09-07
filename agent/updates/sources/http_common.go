package sources

import (
	"context"
	"fmt"
	"io"
	"net/http"
)

// httpGet issues a GET request and returns the body reader on 2xx, or an
// error including the status code and a bounded snippet of the body
// otherwise. The caller owns closing the returned ReadCloser on success.
func httpGet(ctx context.Context, client *http.Client, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request for %s: %w", url, err)
	}
	req.Header.Set("Accept", "application/octet-stream, application/json;q=0.9, */*;q=0.1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", url, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("GET %s: unexpected status %s: %s", url, resp.Status, snippet)
	}
	return resp.Body, nil
}

func defaultClient(c *http.Client) *http.Client {
	if c != nil {
		return c
	}
	return &http.Client{}
}
