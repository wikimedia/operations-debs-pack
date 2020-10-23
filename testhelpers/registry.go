package testhelpers

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	dockertypes "github.com/docker/docker/api/types"
	dockercontainer "github.com/docker/docker/api/types/container"
	"github.com/docker/go-connections/nat"
	"golang.org/x/crypto/bcrypt"
	"gopkg.in/src-d/go-git.v4"
	"gopkg.in/src-d/go-git.v4/plumbing/object"

	"github.com/buildpacks/pack/internal/archive"
)

var registryContainerNames = map[string]string{
	"linux":   "library/registry:2",
	"windows": "micahyoung/registry:latest",
}

type TestRegistryConfig struct {
	runRegistryName string
	RunRegistryHost string
	RunRegistryPort string
	DockerConfigDir string
	username        string
	password        string
}

func RegistryHost(host, port string) string {
	return fmt.Sprintf("%s:%s", host, port)
}

func CreateRegistryFixture(t *testing.T, tmpDir, fixturePath string) string {
	// copy fixture to temp dir
	registryFixtureCopy := filepath.Join(tmpDir, "registryCopy")

	RecursiveCopyNow(t, fixturePath, registryFixtureCopy)

	// git init that dir
	repository, err := git.PlainInit(registryFixtureCopy, false)
	AssertNil(t, err)

	// git add . that dir
	worktree, err := repository.Worktree()
	AssertNil(t, err)

	_, err = worktree.Add(".")
	AssertNil(t, err)

	// git commit that dir
	commit, err := worktree.Commit("first", &git.CommitOptions{
		Author: &object.Signature{
			Name:  "John Doe",
			Email: "john@doe.org",
			When:  time.Now(),
		},
	})
	AssertNil(t, err)

	_, err = repository.CommitObject(commit)
	AssertNil(t, err)

	return registryFixtureCopy
}

func RunRegistry(t *testing.T) *TestRegistryConfig {
	t.Log("run registry")
	t.Helper()

	runRegistryName := "test-registry-" + RandString(10)
	username := RandString(10)
	password := RandString(10)

	runRegistryHost, runRegistryPort := startRegistry(t, runRegistryName, username, password)
	dockerConfigDir := setupDockerConfigWithAuth(t, username, password, runRegistryHost, runRegistryPort)

	registryConfig := &TestRegistryConfig{
		runRegistryName: runRegistryName,
		RunRegistryHost: runRegistryHost,
		RunRegistryPort: runRegistryPort,
		DockerConfigDir: dockerConfigDir,
		username:        username,
		password:        password,
	}

	waitForRegistryToBeAvailable(t, registryConfig)

	return registryConfig
}

func waitForRegistryToBeAvailable(t *testing.T, registryConfig *TestRegistryConfig) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for {
		_, err := registryConfig.RegistryCatalog()
		if err == nil {
			break
		}

		ctxErr := ctx.Err()
		if ctxErr != nil {
			t.Fatal("registry not ready:", ctxErr.Error(), ":", err.Error())
		}

		time.Sleep(500 * time.Microsecond)
	}
}

func (rc *TestRegistryConfig) AuthConfig() dockertypes.AuthConfig {
	return dockertypes.AuthConfig{
		Username:      rc.username,
		Password:      rc.password,
		ServerAddress: RegistryHost(rc.RunRegistryHost, rc.RunRegistryPort),
	}
}

func (rc *TestRegistryConfig) Login(t *testing.T, username string, password string) {
	Eventually(t, func() bool {
		_, err := dockerCli(t).RegistryLogin(context.Background(), dockertypes.AuthConfig{
			Username:      username,
			Password:      password,
			ServerAddress: RegistryHost(rc.RunRegistryHost, rc.RunRegistryPort),
		})
		return err == nil
	}, 100*time.Millisecond, 10*time.Second)
}

func startRegistry(t *testing.T, runRegistryName, username, password string) (string, string) {
	ctx := context.Background()

	daemonInfo, err := dockerCli(t).Info(ctx)
	AssertNil(t, err)

	registryContainerName := registryContainerNames[daemonInfo.OSType]
	AssertNil(t, PullImageWithAuth(dockerCli(t), registryContainerName, ""))

	htpasswdTar := generateHtpasswd(t, username, password)
	defer htpasswdTar.Close()

	ctr, err := dockerCli(t).ContainerCreate(ctx, &dockercontainer.Config{
		Image:  registryContainerName,
		Labels: map[string]string{"author": "pack"},
		Env: []string{
			"REGISTRY_AUTH=htpasswd",
			"REGISTRY_AUTH_HTPASSWD_REALM=Registry Realm",
			"REGISTRY_AUTH_HTPASSWD_PATH=/registry_test_htpasswd",
		},
	}, &dockercontainer.HostConfig{
		AutoRemove: true,
		PortBindings: nat.PortMap{
			"5000/tcp": []nat.PortBinding{{}},
		},
	}, nil, runRegistryName)
	AssertNil(t, err)
	err = dockerCli(t).CopyToContainer(ctx, ctr.ID, "/", htpasswdTar, dockertypes.CopyToContainerOptions{})
	AssertNil(t, err)

	err = dockerCli(t).ContainerStart(ctx, ctr.ID, dockertypes.ContainerStartOptions{})
	AssertNil(t, err)
	inspect, err := dockerCli(t).ContainerInspect(context.TODO(), ctr.ID)
	AssertNil(t, err)
	runRegistryPort := inspect.NetworkSettings.Ports["5000/tcp"][0].HostPort

	runRegistryHost := DockerHostname(t)

	return runRegistryHost, runRegistryPort
}

func DockerHostname(t *testing.T) string {
	dockerCli := dockerCli(t)

	daemonHost := dockerCli.DaemonHost()
	u, err := url.Parse(daemonHost)
	if err != nil {
		t.Fatalf("unable to parse URI client.DaemonHost: %s", err)
	}

	switch u.Scheme {
	// DOCKER_HOST is usually remote so always use its hostname/IP
	// Note: requires "insecure-registries" CIDR entry on Daemon config
	case "tcp":
		return u.Hostname()

	// if DOCKER_HOST is non-tcp, we assume that we are
	// talking to the daemon over a local pipe.
	default:
		daemonInfo, err := dockerCli.Info(context.TODO())
		if err != nil {
			t.Fatalf("unable to fetch client.DockerInfo: %s", err)
		}

		if daemonInfo.OSType == "windows" {
			// try to lookup the host IP by helper domain name (https://docs.docker.com/docker-for-windows/networking/#use-cases-and-workarounds)
			// Note: pack appears to not support /etc/hosts-based insecure-registries
			addrs, err := net.LookupHost("host.docker.internal")
			if err != nil {
				t.Fatalf("unknown address response: %+v %s", addrs, err)
			}
			if len(addrs) != 1 {
				t.Fatalf("ambiguous address response: %v", addrs)
			}
			return addrs[0]
		}

		// Linux can use --network=host so always use "localhost"
		return "localhost"
	}
}

func generateHtpasswd(t *testing.T, username string, password string) io.ReadCloser {
	// https://docs.docker.com/registry/deploying/#restricting-access
	// HTPASSWD format: https://github.com/foomo/htpasswd/blob/e3a90e78da9cff06a83a78861847aa9092cbebdd/hashing.go#L23
	passwordBytes, _ := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	reader := archive.CreateSingleFileTarReader("/registry_test_htpasswd", username+":"+string(passwordBytes))
	return reader
}

func setupDockerConfigWithAuth(t *testing.T, username string, password string, runRegistryHost string, runRegistryPort string) string {
	dockerConfigDir, err := ioutil.TempDir("", "pack.test.docker.config.dir")
	AssertNil(t, err)

	AssertNil(t, ioutil.WriteFile(filepath.Join(dockerConfigDir, "config.json"), []byte(fmt.Sprintf(`{
			  "auths": {
			    "%s": {
			      "auth": "%s"
			    }
			  }
			}
			`, RegistryHost(runRegistryHost, runRegistryPort), encodedUserPass(username, password))), 0666))
	return dockerConfigDir
}

func encodedUserPass(username string, password string) string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("%s:%s", username, password)))
}

func (rc *TestRegistryConfig) StopRegistry(t *testing.T) {
	t.Log("stop registry")
	t.Helper()
	err := dockerCli(t).ContainerKill(context.Background(), rc.runRegistryName, "SIGKILL")
	AssertNil(t, err)

	err = os.RemoveAll(rc.DockerConfigDir)
	AssertNil(t, err)
}

func (rc *TestRegistryConfig) RepoName(name string) string {
	return RegistryHost(rc.RunRegistryHost, rc.RunRegistryPort) + "/" + name
}

func (rc *TestRegistryConfig) RegistryAuth() string {
	return base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf(`{"username":"%s","password":"%s"}`, rc.username, rc.password)))
}

func (rc *TestRegistryConfig) RegistryCatalog() (string, error) {
	return HTTPGetE(fmt.Sprintf("http://%s/v2/_catalog", RegistryHost(rc.RunRegistryHost, rc.RunRegistryPort)), map[string]string{
		"Authorization": "Basic " + encodedUserPass(rc.username, rc.password),
	})
}
