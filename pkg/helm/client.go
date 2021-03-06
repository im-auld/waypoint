package helm

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"bytes"

	"github.com/kylie-a/requests"
	"github.com/mitchellh/go-homedir"
	"k8s.io/helm/pkg/chartutil"
	"k8s.io/helm/pkg/getter"
	"k8s.io/helm/pkg/helm"
	"k8s.io/helm/pkg/helm/environment"
	"k8s.io/helm/pkg/helm/helmpath"
	"k8s.io/helm/pkg/proto/hapi/chart"
	rls "k8s.io/helm/pkg/proto/hapi/services"
	"k8s.io/helm/pkg/renderutil"
	"k8s.io/helm/pkg/repo"
)

const chartsAPI = "/api/charts"

type Client struct {
	tillerClient *helm.Client
	tillerOpts   []helm.Option
	env          environment.EnvSettings
	token        string
	debug        bool
}

func NewClient(opts ...HelmOption) *Client {
	var err error
	var homeDir string

	tillerClient := helm.NewClient(helm.Host("localhost:50000"))

	helmHome := ".helm"
	if homeDir, err = homedir.Dir(); err != nil {
		homeDir = os.Getenv("HOME")
	}
	env := environment.EnvSettings{
		Home: helmpath.Home(filepath.Join(homeDir, helmHome)),
	}
	client := &Client{
		tillerClient: tillerClient,
		env:          env,
		debug: false,
	}
	for _, opt := range opts {
		opt(client)
	}
	client.tillerClient.Option(client.tillerOpts...)
	return client
}

func (c *Client) RemoveChart(app, repoName, version string) error {
	var err error
	var url string
	var resp *requests.Response

	if url, err = c.getHelmDeleteURL(app, repoName, version); err != nil {
		return err
	}
	if resp, err = requests.Delete(url); err != nil {
		return err
	}
	if resp.Code != 200 {
		return NewHelmDeleteError(resp.Content())
	}
	return nil
}

func (c *Client) UploadChart(ch []byte, repoName string) error {
	var resp *requests.Response
	var err error
	var url string

	if url, err = c.getHelmPostURL(repoName); err != nil {
		return err
	}
	if resp, err = requests.Post(url, bytes.NewBuffer(ch)); err != nil {
		return err
	}
	if resp.Code <= 199 || resp.Code >= 300 {
		return NewHelmUploadError(resp.Content())
	}
	return nil
}

func (c *Client) HasChart(app, repoName, version string) bool {
	var resp *requests.Response
	var err error

	url, err := c.getHelmDeleteURL(app, repoName, version)
	if resp, err = requests.Get(url); err != nil {
		return false
	}
	return resp.Code == 200
}

func (c *Client) Package(src, version, dest string, saveLocal bool) error {
	var ch *chart.Chart
	var err error

	if ch, err = chartutil.LoadDir(src); err != nil {
		return err
	}

	ch.Metadata.Version = version

	if filepath.Base(src) != ch.Metadata.Name {
		return fmt.Errorf(
			"directory name (%s) and Chart.yaml name (%s) must match",
			filepath.Base(src),
			ch.Metadata.Name,
		)
	}
	if reqs, err := chartutil.LoadRequirements(ch); err == nil {
		if err := renderutil.CheckDependencies(ch, reqs); err != nil {
			return err
		}
	} else {
		if err != chartutil.ErrRequirementsNotFound {
			return err
		}
	}

	if dest == "." {
		// Save to the current working directory.
		dest, err = os.Getwd()
		if err != nil {
			return err
		}
	}

	if _, err = chartutil.Save(ch, dest); err != nil {
		return fmt.Errorf("failed to save: %s", err)
	}

	if saveLocal {
		lr := c.env.Home.LocalRepository()
		if err := repo.AddChartToLocalRepo(ch, lr); err != nil {
			return err
		}
	}

	return err
}

func (c *Client) Install(src, ns string, opts map[string]interface{}) error {
	var ch *chart.Chart
	var err error

	if ch, err = chartutil.LoadFile(src); err != nil {
		return err
	}
	// installOpts := optMap.getOptions(opts)
	if _, err = c.tillerClient.InstallReleaseFromChart(ch, ns); err != nil {
		return err
	}
	return nil
}

func (c *Client) Upgrade(app, src string, opts map[string]interface{}) error {
	//var ch *chart.Chart
	var rel *rls.UpdateReleaseResponse
	var err error

	if rel, err = c.tillerClient.UpdateRelease(app, src); err != nil {
		return err
	}

	if c.debug {
		printRelease(os.Stdout, rel.Release)
	}
	return nil
}

func (c *Client) UpdateRepo(repoName string) error {
	var entry *repo.ChartRepository
	var err error

	if entry, err = c.getRepo(repoName); err != nil {
		return err
	}
	c.updateRepo(entry, c.env.Home)
	return nil
}

func (c *Client) UpdateRepos() error {
	var f *repo.RepoFile
	var r *repo.ChartRepository
	var err error

	if f, err = c.getHelmRepos(); err == nil {
		var repos []*repo.ChartRepository
		for _, entry := range f.Repositories {
			if r, err = repo.NewChartRepository(entry, getter.All(c.env)); err != nil {
				return err
			}
			repos = append(repos, r)
		}
		c.updateRepos(repos, c.env.Home)
	}
	return err
}

func (c *Client) UpdateIndex(chartSrc, baseURL string) error {
	path, err := filepath.Abs(chartSrc)
	if err != nil {
		return err
	}
	return c.index(path, baseURL, "")
}

func (c *Client) getHelmRepos() (*repo.RepoFile, error) {
	f, err := repo.LoadRepositoriesFile(c.env.Home.RepositoryFile())
	if err != nil {
		return nil, NewHelmRepoFileLoadError(err.Error())
	}
	if len(f.Repositories) == 0 {
		return nil, NewNoHelmReposError()
	}
	return f, nil
}

func (c *Client) getRepo(name string) (*repo.ChartRepository, error) {
	var f *repo.RepoFile
	var err error

	if f, err = c.getHelmRepos(); err != nil {
		return nil, err
	}
	for _, re := range f.Repositories {
		if re.Name == name {
			return repo.NewChartRepository(re, getter.All(c.env))
		}
	}
	return nil, NewHelmRepoNotFoundError(name)
}

func (c *Client) getHelmURL(repoName string) (string, error) {
	var f *repo.RepoFile
	var err error

	if f, err = c.getHelmRepos(); err != nil {
		return "", err
	}
	for _, entry := range f.Repositories {
		if entry.Name == repoName {
			return entry.URL, nil
		}
	}
	return "", NewHelmRepoNotFoundError(repoName)
}

func (c *Client) getHelmDeleteURL(app, repoName, version string) (string, error) {
	var err error
	if url, err := c.getHelmURL(repoName); err == nil {
		return fmt.Sprintf("%s%s/%s/%s", url, chartsAPI, app, version), nil
	}
	return "", err
}

func (c *Client) getHelmPostURL(repoName string) (string, error) {
	var err error
	if url, err := c.getHelmURL(repoName); err == nil {
		return fmt.Sprintf("%s%s", url, chartsAPI), nil
	}
	return "", err
}

func (c *Client) updateRepo(repo *repo.ChartRepository, home helmpath.Home) {
	if repo.Config.Name == "local" {
		return
	}
	if err := repo.DownloadIndexFile(home.Cache()); err != nil {
		fmt.Println(err.Error())
	}
}

func (c *Client) updateRepos(repos []*repo.ChartRepository, home helmpath.Home) {
	var wg sync.WaitGroup
	for _, re := range repos {
		wg.Add(1)
		go func(re *repo.ChartRepository) {
			defer wg.Done()
			c.updateRepo(re, home)
		}(re)
	}
	wg.Wait()
}

func (c *Client) index(dir, url, mergeTo string) error {
	out := filepath.Join(dir, "index.yaml")

	i, err := repo.IndexDirectory(dir, url)
	if err != nil {
		return err
	}
	if mergeTo != "" {
		// if index.yaml is missing then create an empty one to merge into
		var i2 *repo.IndexFile
		if _, err := os.Stat(mergeTo); os.IsNotExist(err) {
			i2 = repo.NewIndexFile()
			if err := i2.WriteFile(mergeTo, 0644); err != nil {
				return err
			}
		} else {
			i2, err = repo.LoadIndexFile(mergeTo)
			if err != nil {
				return fmt.Errorf("merge failed: %s", err)
			}
		}
		i.Merge(i2)
	}
	i.SortEntries()
	return i.WriteFile(out, 0644)
}
