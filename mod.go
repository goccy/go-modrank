package modrank

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/gocolly/colly"

	"github.com/goccy/go-modrank/repository"
)

// GoModule represents of the state of the Go module used in a given repository.
type GoModule struct {
	// ID to uniquely identify a GoModule, hashed from Repository/GoModPath/Name/Version.
	ID string
	// Repository name of the repository using this Go module.
	Repository string
	// GoModPath path to the go.mod on the repository using this Go module.
	GoModPath string
	// Name is the Go module name. e.g.) github.com/goccy/go-modrank
	Name string
	// Version is the version text for the Go module. e.g.) v1.2.3
	Version string
	// HostedRepository is the hosted repository name of the Go module.
	HostedRepository string
	// Refers is the list of Modules this Go module depends on.
	Refers []*GoModule
	// Referers is th list of Modules on which this Go module is dependent.
	Referers []*GoModule

	referMap   map[*GoModule]struct{}
	refererMap map[*GoModule]struct{}
}

// ModPath returns "Name@Version" format.
func (m *GoModule) ModPath() string {
	return m.Name + "@" + m.Version
}

// IsRoot returns whether this Go module is first dependent module or not.
func (m *GoModule) IsRoot() bool {
	return len(m.Referers) == 0
}

func (m *GoModule) setupReference() {
	m.setRefers()
	m.setReferers()
}

func (m *GoModule) setRefers() {
	var v []*GoModule
	for mod := range m.referMap {
		v = append(v, mod)
	}
	sort.Slice(v, func(i, j int) bool {
		return v[i].ModPath() < v[j].ModPath()
	})
	m.Refers = v
}

func (m *GoModule) setReferers() {
	var v []*GoModule
	for mod := range m.refererMap {
		v = append(v, mod)
	}
	sort.Slice(v, func(i, j int) bool {
		return v[i].ModPath() < v[j].ModPath()
	})
	m.Referers = v
}

func newGoModule(repo *repository.Repository, goModPath, rootModName, modPath string, modCache map[string]*GoModule) (*GoModule, error) {
	if rootModName == modPath {
		// root module
		return nil, nil
	}
	if node, exists := modCache[modPath]; exists {
		return node, nil
	}

	name, ver, err := splitModNameAndVersion(modPath)
	if err != nil {
		return nil, err
	}
	if name == "go" || name == "toolchain" {
		// keyword
		return nil, nil
	}
	hash := sha256.Sum256([]byte(fmt.Sprintf("%s/%s/%s/%s", repo.NameWithOwner(), goModPath, name, ver)))
	node := &GoModule{
		ID:               hex.EncodeToString(hash[:]),
		Repository:       repo.NameWithOwner(),
		GoModPath:        goModPath,
		Name:             name,
		Version:          ver,
		HostedRepository: getHostedRepositoryByNameWithCache(name),
		referMap:         make(map[*GoModule]struct{}),
		refererMap:       make(map[*GoModule]struct{}),
	}
	modCache[modPath] = node
	return node, nil
}

func getHostedRepositoryByNameWithCache(name string) string {
	normalized := normalizeGoModuleName(name)
	if repo := getHostedRepositoryByCache(normalized); repo != "" {
		return repo
	}
	ret := getHostedRepositoryByName(normalized)
	setHostedRepositoryCache(normalized, ret)
	return ret
}

func getHostedRepositoryByName(name string) string {
	if repo, _ := getHostedRepositoryByGoProxy(name); repo != "" {
		return repo
	}
	if repo, _ := getHostedRepositoryByGoPkgIn(name); repo != "" {
		return repo
	}
	if repo, _ := getHostedRepositoryByGoImportMetaTag(name); repo != "" {
		return repo
	}
	return name
}

var (
	hostRepoCache   = make(map[string]string)
	hostRepoCacheMu sync.RWMutex
)

func getHostedRepositoryByCache(name string) string {
	hostRepoCacheMu.RLock()
	defer hostRepoCacheMu.RUnlock()
	return hostRepoCache[name]
}

func setHostedRepositoryCache(key, value string) {
	hostRepoCacheMu.Lock()
	defer hostRepoCacheMu.Unlock()
	hostRepoCache[key] = value
}

func normalizeGoModuleName(name string) string {
	parts := strings.Split(name, "/")
	if len(parts) <= 3 {
		return name
	}
	return strings.Join(parts[:3], "/")
}

func getHostedRepositoryByGoProxy(name string) (string, error) {
	resp, err := http.Get(fmt.Sprintf("https://proxy.golang.org/%s/@latest", name))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("failed to call proxy.golang.org: %s: %s", resp.Status, string(body))
	}

	var v struct {
		Origin struct {
			URL string
		}
	}
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return "", fmt.Errorf("failed to decode %s module: %w", name, err)
	}
	return strings.TrimPrefix(v.Origin.URL, "https://"), nil
}

func getHostedRepositoryByGoImportMetaTag(name string) (string, error) {
	c := colly.NewCollector()
	var repo string
	c.OnHTML("meta[name=go-import]", func(e *colly.HTMLElement) {
		content := e.Attr("content")
		if content == "" {
			return
		}
		parts := strings.Split(content, " ")
		if len(parts) != 3 {
			return
		}
		repo = strings.TrimSuffix(parts[2], ".git")
	})
	if err := c.Visit(fmt.Sprintf("https://%s?go-get=1", name)); err != nil {
		return "", err
	}
	return strings.TrimPrefix(repo, "https://"), nil
}

var (
	gopkgInPat          = regexp.MustCompile(`gopkg.in/(.+)\.v[0-9]+$`)
	gopkgInWithOwnerPat = regexp.MustCompile(`gopkg.in/(.+)/(.+)\.v[0-9]+$`)
)

func getHostedRepositoryByGoPkgIn(name string) (string, error) {
	{
		matched := gopkgInWithOwnerPat.FindAllStringSubmatch(name, -1)
		if len(matched) != 0 {
			if len(matched[0]) == 3 {
				owner := matched[0][1]
				pkg := matched[0][2]
				return fmt.Sprintf("github.com/%s/%s", owner, pkg), nil
			}
		}
	}
	{
		matched := gopkgInPat.FindAllStringSubmatch(name, -1)
		if len(matched) != 0 {
			if len(matched[0]) == 2 {
				pkg := matched[0][1]
				return fmt.Sprintf("github.com/go-%[1]s/%[1]s", pkg), nil
			}
		}
	}
	return "", nil
}

func splitModNameAndVersion(mod string) (string, string, error) {
	parts := strings.Split(mod, "@")
	if len(parts) != 2 {
		return "", "", fmt.Errorf("unexpected go module format: %s", mod)
	}
	return parts[0], parts[1], nil
}
