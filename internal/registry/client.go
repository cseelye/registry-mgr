package registry

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cseelye/registry-mgr/internal/models"
)

// ErrUnauthorized is returned when the registry rejects credentials.
var ErrUnauthorized = fmt.Errorf("authentication failed: check registry credentials")

// manifestAccept is the Accept header value for manifest requests.
// Includes both OCI (registry v3 default) and Docker v2 formats.
const manifestAccept = "application/vnd.oci.image.manifest.v1+json" +
	",application/vnd.docker.distribution.manifest.v2+json"

// Client interacts with a Docker Registry HTTP API V2.
type Client struct {
	baseURL    string
	username   string
	password   string
	httpClient *http.Client
}

// NewClient creates a new registry client.
func NewClient(baseURL, username, password string) *Client {
	return &Client{
		baseURL:    strings.TrimRight(baseURL, "/"),
		username:   username,
		password:   password,
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (c *Client) do(ctx context.Context, method, path string, headers map[string]string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	if c.username != "" {
		req.SetBasicAuth(c.username, c.password)
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	return c.httpClient.Do(req)
}

// Ping checks basic connectivity and authentication against the registry.
func (c *Client) Ping(ctx context.Context) error {
	resp, err := c.do(ctx, "GET", "/v2/", nil)
	if err != nil {
		return fmt.Errorf("connecting to registry: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusUnauthorized {
		return fmt.Errorf("registry returned unexpected status %d", resp.StatusCode)
	}
	return nil
}

// ListRepositories returns all repository names in the registry.
func (c *Client) ListRepositories(ctx context.Context) ([]string, error) {
	var repos []string
	path := "/v2/_catalog"
	for path != "" {
		resp, err := c.do(ctx, "GET", path, nil)
		if err != nil {
			return nil, fmt.Errorf("listing repositories: %w", err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading catalog response: %w", err)
		}
		if err := checkAuth(resp.StatusCode); err != nil {
			return nil, err
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("listing repositories: unexpected status %d", resp.StatusCode)
		}
		var result struct {
			Repositories []string `json:"repositories"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decoding repository list: %w", err)
		}
		repos = append(repos, result.Repositories...)
		path = nextLink(resp)
	}
	return repos, nil
}

// ListTags returns all tags for a repository.
func (c *Client) ListTags(ctx context.Context, repo string) ([]string, error) {
	var tags []string
	path := fmt.Sprintf("/v2/%s/tags/list", repo)
	for path != "" {
		resp, err := c.do(ctx, "GET", path, nil)
		if err != nil {
			return nil, fmt.Errorf("listing tags for %s: %w", repo, err)
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("reading tags response: %w", err)
		}
		if err := checkAuth(resp.StatusCode); err != nil {
			return nil, err
		}
		if resp.StatusCode == http.StatusNotFound {
			return nil, nil
		}
		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("listing tags for %s: unexpected status %d", repo, resp.StatusCode)
		}
		var result struct {
			Tags []string `json:"tags"`
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return nil, fmt.Errorf("decoding tag list: %w", err)
		}
		tags = append(tags, result.Tags...)
		path = nextLink(resp)
	}
	return tags, nil
}

// GetImageDetails fetches manifest and config blob to return full image details.
func (c *Client) GetImageDetails(ctx context.Context, repo, tag string) (*models.Image, error) {
	headers := map[string]string{
		"Accept": manifestAccept,
	}
	resp, err := c.do(ctx, "GET", fmt.Sprintf("/v2/%s/manifests/%s", repo, tag), headers)
	if err != nil {
		return nil, fmt.Errorf("getting manifest for %s:%s: %w", repo, tag, err)
	}
	body, err := io.ReadAll(resp.Body)
	resp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("reading manifest: %w", err)
	}
	if err := checkAuth(resp.StatusCode); err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("getting manifest for %s:%s: unexpected status %d", repo, tag, resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")

	var manifest struct {
		Config struct {
			Digest string `json:"digest"`
		} `json:"config"`
		Layers []struct {
			Size int64 `json:"size"`
		} `json:"layers"`
	}
	if err := json.Unmarshal(body, &manifest); err != nil {
		return nil, fmt.Errorf("parsing manifest: %w", err)
	}

	var totalSize int64
	for _, layer := range manifest.Layers {
		totalSize += layer.Size
	}

	// Fetch image config blob for creation time, OS, arch, labels.
	configResp, err := c.do(ctx, "GET", fmt.Sprintf("/v2/%s/blobs/%s", repo, manifest.Config.Digest), nil)
	if err != nil {
		return nil, fmt.Errorf("getting config blob for %s:%s: %w", repo, tag, err)
	}
	configBody, err := io.ReadAll(configResp.Body)
	configResp.Body.Close()
	if err != nil {
		return nil, fmt.Errorf("reading config blob: %w", err)
	}

	var imageConfig struct {
		Architecture string    `json:"architecture"`
		OS           string    `json:"os"`
		Created      time.Time `json:"created"`
		Config       struct {
			Labels map[string]string `json:"Labels"`
		} `json:"config"`
	}
	if err := json.Unmarshal(configBody, &imageConfig); err != nil {
		return nil, fmt.Errorf("parsing image config: %w", err)
	}

	return &models.Image{
		Repository: repo,
		Tag:        tag,
		Digest:     digest,
		Size:       totalSize,
		CreatedAt:  imageConfig.Created,
		OS:         imageConfig.OS,
		Arch:       imageConfig.Architecture,
		Labels:     imageConfig.Config.Labels,
	}, nil
}

// DeleteTag resolves a tag to its manifest digest and deletes it.
func (c *Client) DeleteTag(ctx context.Context, repo, tag string) error {
	headers := map[string]string{
		"Accept": manifestAccept,
	}
	resp, err := c.do(ctx, "HEAD", fmt.Sprintf("/v2/%s/manifests/%s", repo, tag), headers)
	if err != nil {
		return fmt.Errorf("resolving digest for %s:%s: %w", repo, tag, err)
	}
	resp.Body.Close()
	if err := checkAuth(resp.StatusCode); err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("resolving digest for %s:%s: unexpected status %d", repo, tag, resp.StatusCode)
	}

	digest := resp.Header.Get("Docker-Content-Digest")
	if digest == "" {
		return fmt.Errorf("no digest returned for %s:%s", repo, tag)
	}

	delResp, err := c.do(ctx, "DELETE", fmt.Sprintf("/v2/%s/manifests/%s", repo, digest), nil)
	if err != nil {
		return fmt.Errorf("deleting %s:%s: %w", repo, tag, err)
	}
	delResp.Body.Close()
	if delResp.StatusCode != http.StatusAccepted {
		return fmt.Errorf("deleting %s:%s: unexpected status %d", repo, tag, delResp.StatusCode)
	}
	return nil
}

// checkAuth returns ErrUnauthorized if the status code indicates an auth failure.
func checkAuth(status int) error {
	if status == http.StatusUnauthorized || status == http.StatusForbidden {
		return ErrUnauthorized
	}
	return nil
}

// nextLink parses a RFC 5988 Link header and returns the path for rel="next", or "".
func nextLink(resp *http.Response) string {
	link := resp.Header.Get("Link")
	if link == "" {
		return ""
	}
	for _, part := range strings.Split(link, ",") {
		part = strings.TrimSpace(part)
		sections := strings.Split(part, ";")
		if len(sections) < 2 {
			continue
		}
		if strings.TrimSpace(sections[1]) == `rel="next"` {
			rawURL := strings.Trim(strings.TrimSpace(sections[0]), "<>")
			if u, err := url.Parse(rawURL); err == nil {
				return u.RequestURI()
			}
		}
	}
	return ""
}
