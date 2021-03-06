package docker

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"strings"

	credClient "github.com/docker/docker-credential-helpers/client"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/archive"
	"github.com/mitchellh/go-homedir"
	"github.com/pkg/errors"
)

type Client struct {
	dkr *client.Client
}

func NewDockerClient() (*Client, error) {
	var cli *client.Client
	var err error

	if cli, err = client.NewEnvClient(); err != nil {
		return nil, err
	}
	return &Client{dkr: cli}, nil
}

func (c *Client) getDockerListOpts(image string) types.ImageListOptions {
	a := filters.NewArgs()
	a.Add("reference", image)
	return types.ImageListOptions{Filters: a}
}

func (c *Client) RemoveImage(taggedImageName string) error {
	var list []types.ImageSummary
	var err error

	listOpts := c.getDockerListOpts(taggedImageName)
	if list, err = c.dkr.ImageList(context.Background(), listOpts); err != nil {
		return err
	}

	var img types.ImageSummary
	if len(list) == 1 {
		img = list[0]
	} else {
		return errors.New(fmt.Sprintf("%d images found; skipping", len(list)))
	}
	_, err = c.dkr.ImageRemove(context.Background(), img.ID, types.ImageRemoveOptions{})
	return err
}

func (c *Client) BuildImage(taggedImageName, buildCtx string) error {
	var err error
	var body types.ImageBuildResponse
	var ctx io.Reader

	if ctx, err = c.GetContext(buildCtx); err != nil {
		return err
	}
	opts := types.ImageBuildOptions{
		Tags: []string{taggedImageName},
	}
	if body, err = c.dkr.ImageBuild(context.Background(), ctx, opts); err != nil {
		return err
	}
	defer body.Body.Close()
	if _, err = ioutil.ReadAll(body.Body); err != nil {
		return err
	}
	return nil
}

func (c *Client) PushImage(ref, repo, credHelper string) error {
	var err error
	var body io.ReadCloser
	var creds string

	if creds, err = c.Auth(repo, credHelper); err != nil {
		return err
	}
	opts := types.ImagePushOptions{RegistryAuth: creds}
	if body, err = c.dkr.ImagePush(context.Background(), ref, opts); err != nil {
		return err
	}
	defer body.Close()
	if _, err = ioutil.ReadAll(body); err != nil {
		// {"errorDetail":{"message":"unauthorized: You don't have the needed permissions to perform this operation, and you may have invalid credentials. To authenticate your request, follow the steps in: https://cloud.google.com/container-registry/docs/advanced-authentication"},"error":"unauthorized: You don't have the needed permissions to perform this operation, and you may have invalid credentials. To authenticate your request, follow the steps in: https://cloud.google.com/container-registry/docs/advanced-authentication"}
		return err
	}
	return nil
}

func (c *Client) Auth(repo, credHelper string) (string, error) {
	var encodedJSON []byte
	var err error

	server := strings.Split(repo, "/")[0]
	creds, err := credClient.Get(credClient.NewShellProgramFunc(credHelper), fmt.Sprintf("https://%s", server))
	if err != nil {
		fmt.Println()
		return "", err
	}
	authConfig := types.AuthConfig{
		Username: creds.Username,
		Password: creds.Secret,
	}
	if encodedJSON, err = json.Marshal(authConfig); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(encodedJSON), nil
}

func (c *Client) GetContext(buildCtx string) (io.Reader, error) {
	ctxDir, err := homedir.Expand(buildCtx)
	if err != nil {
		return nil, err
	}

	return archive.TarWithOptions(ctxDir, &archive.TarOptions{})
}
