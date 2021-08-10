package docker

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/pkg/errors"
)

type Client struct {
	cli *client.Client
}

func NewClient() (Client, error) {
	c, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return Client{}, err
	}

	return Client{
		cli: c,
	}, nil
}

func (s Client) CreateContainer(name, image, deployment string) (string, error) {
	fmt.Printf("Creating a new container %s (%s) for deployment %s.\n", name, image, deployment) // TODO debug

	// TODO support container.Config.Env

	cont, err := s.cli.ContainerCreate(
		context.Background(),
		&container.Config{
			Image:        image,
			AttachStdout: true,
			AttachStderr: true,
			Labels: map[string]string{
				"orchestrator": "docker-fpm",
				"deployment":   deployment,
			},
		},
		&container.HostConfig{
			Privileged: false,
			// Resources: container.Resources{}, // TODO allow specifying these
			// TODO mount support
			/*Mounts: []mount.Mount{
				{
					Type:   mount.TypeBind,
					Source: "/foo/source/dir",
					Target: "/samp",
				},
			},*/
		},
		&network.NetworkingConfig{},
		nil,
		name,
	)

	if err != nil {
		return "", errors.Wrap(err, "Unable to create a new container")
	}

	if len(cont.Warnings) > 0 {
		fmt.Printf("%d warnings for created container %s:\n", len(cont.Warnings), cont.ID)
		for _, warn := range cont.Warnings {
			fmt.Println(warn)
		}
	}

	return cont.ID, nil
}

func (s Client) StartContainer(id string) error {
	fmt.Printf("Starting container %s...\n", id) // TODO debug

	if err := s.cli.ContainerStart(context.Background(), id, types.ContainerStartOptions{}); err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to start container %s", id))
	}

	return nil
}

func (s Client) ContainerDetails(id string) (types.ContainerJSON, error) {
	details, err := s.cli.ContainerInspect(context.Background(), id)
	if err != nil {
		return types.ContainerJSON{}, errors.Wrap(err, fmt.Sprintf("Unable to fetch details for container %s", id))
	}

	return details, nil
}

func (s Client) listFilteredContainers(filters filters.Args) ([]types.Container, error) {
	containers, err := s.cli.ContainerList(context.Background(), types.ContainerListOptions{
		Filters: filters,
	})

	if err != nil {
		return nil, errors.Wrap(err, "Unable to list containers")
	}

	return containers, nil
}

func (s Client) ListAllContainers() ([]types.Container, error) {
	filters := filters.Args{}
	filters.Add("label", "orchestrator=docker-fpm")

	return s.listFilteredContainers(filters)
}

func (s Client) ListDeploymentContainers(deployment string) ([]types.Container, error) {
	filters := filters.Args{}
	filters.Add("label", "orchestrator=docker-fpm")
	filters.Add("label", fmt.Sprintf("deployment=%s", deployment))

	return s.listFilteredContainers(filters)
}

func (s Client) StopContainer(id string) error {
	fmt.Printf("Stopping container %s...\n", id) // TODO debug

	if err := s.cli.ContainerStop(context.Background(), id, nil); err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to stop container %s", id))
	}

	return nil
}

func (s Client) KillContainer(id string) error {
	fmt.Printf("Killing container %s...\n", id) // TODO debug

	if err := s.cli.ContainerKill(context.Background(), id, "SIGKILL"); err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to kill container %s", id))
	}

	return nil
}

func (s Client) RemoveContainer(id string) error {
	fmt.Printf("Removing container %s...\n", id) // TODO debug

	if err := s.cli.ContainerRemove(
		context.Background(),
		id,
		types.ContainerRemoveOptions{
			RemoveVolumes: false,
			Force:         false,
		},
	); err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to remove container %s", id))
	}

	return nil
}
