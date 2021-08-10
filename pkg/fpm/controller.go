package fpm

import (
	"fmt"
	"github.com/ajmyyra/docker-fpm/pkg/docker"
	"github.com/pkg/errors"
	"io"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

const DynamicController = "dynamic"
const StaticController = "static"

var controllerTypes = []string{DynamicController, StaticController}

type ControllerConfig struct {
	Deployment        string
	ContainerImage    string
	ContainerImageTag string
	ContainerPort     int
	ContainerAmount   int
	Type              string
	DynIdleSeconds    int
}

type Container struct {
	Name    string
	Id      string
	Started bool
	Dirty   bool
	IPAddr  string
}

type ReqController struct {
	DockerCli   docker.Client
	Config      ControllerConfig
	Containers  []Container
	ContainerNo int
	LastReq     time.Time
	Lock        *sync.RWMutex
}

func DefaultConfig(deployment, image, tag string, port int) ControllerConfig {
	return ControllerConfig{
		Deployment:        deployment,
		ContainerImage:    image,
		ContainerImageTag: tag,
		ContainerPort:     port,
		ContainerAmount:   1,
		Type:              "dynamic",
		DynIdleSeconds:    60,
	}
}

func NewReqController(conf ControllerConfig) (ReqController, error) {
	if !validControllerType(conf.Type) {
		return ReqController{}, errors.New(fmt.Sprintf("Invalid controller type: %s", conf.Type))
	}

	adm := ReqController{
		Config:      conf,
		ContainerNo: 0,
		Containers:  []Container{},
		LastReq:     time.Now(),
		Lock:        &sync.RWMutex{},
	}
	cli, err := docker.NewClient()
	if err != nil {
		return ReqController{}, errors.Wrap(err, "Unable to initialize Docker client")
	}
	adm.DockerCli = cli

	return adm, nil
}

func (s *ReqController) createNewContainer() error {
	s.ContainerNo += 1

	cName := fmt.Sprintf("%s-%d", s.Config.Deployment, s.ContainerNo)
	c, err := s.DockerCli.CreateContainer(cName, s.containerImageName(), s.Config.Deployment)
	if err != nil {
		return err
	}

	s.Containers = append(s.Containers, Container{
		Name:    cName,
		Id:      c,
		Started: false,
		IPAddr:  "",
	})

	return nil
}

// This currently starts every configured container. Future work is needed to allow
// smarter ways for starting & stopping containers based on req/min.
func (s *ReqController) startContainers() error {
	for i, c := range s.Containers {
		if c.Started {
			continue
		}

		if err := s.DockerCli.StartContainer(c.Id); err != nil {
			return err
		}

		details, err := s.DockerCli.ContainerDetails(c.Id)
		if err != nil {
			return err
		}

		c.IPAddr = details.NetworkSettings.IPAddress
		c.Started = true
		s.Containers[i] = c
	}

	return nil
}

// This currently stops every configured container. Future work is needed to allow
// smarter ways for starting & stopping containers based on req/min.
func (s *ReqController) stopContainers(hard bool) error {
	for i, c := range s.Containers {
		if !c.Started {
			continue
		}

		if err := s.DockerCli.StopContainer(c.Id); err != nil {
			if !hard {
				return err
			}

			if err = s.DockerCli.KillContainer(c.Id); err != nil {
				return err
			}
		}

		c.Started = false
		c.IPAddr = ""
		s.Containers[i] = c
	}

	return nil
}

func (s *ReqController) cleanupContainers() error {
	for _, c := range s.Containers {
		if c.Started {
			if err := s.DockerCli.KillContainer(c.Id); err != nil {
				return err
			}
		}

		if err := s.DockerCli.RemoveContainer(c.Id); err != nil {
			return err
		}
	}

	return nil
}

func (s *ReqController) getRandomContainer() (Container, error) {
	amount := len(s.Containers)
	if amount == 0 {
		return Container{}, errors.New("No configured containers to choose from")
	}

	// TODO revisit this when some containers can be up or down at the same time in dynamic mode
	for attempts := 1; attempts <= s.Config.ContainerAmount; attempts++ {
		random := rand.Intn(amount)
		candidate := s.Containers[random]
		if !candidate.Dirty && candidate.Started {
			return candidate, nil
		}
	}

	// If quick selection didn't work out, we'll get the first available that matches
	for _, candidate := range s.Containers {
		if !candidate.Dirty && candidate.Started {
			return candidate, nil
		}
	}

	return Container{}, errors.New("All containers are either shut down or marked as dirty")
}

func (s *ReqController) setContainerDirty(id string) {
	for i, c := range s.Containers {
		if c.Id == id {
			c.Dirty = true
			s.Containers[i] = c
		}
	}
}

func (s *ReqController) containerImageName() string {
	return fmt.Sprintf("%s:%s", s.Config.ContainerImage, s.Config.ContainerImageTag)
}

func (s *ReqController) Init() error {
	s.Lock.Lock()
	defer s.Lock.Unlock()

	// Yeah yeah, but we're selecting random containers and not doing cryptography. Come at me, cyberbros.
	rand.Seed(time.Now().UnixNano())

	for i := 0; i < s.Config.ContainerAmount; i++ {
		if err := s.createNewContainer(); err != nil {
			return err
		}
	}

	if s.Config.Type == StaticController {
		if err := s.startContainers(); err != nil {
			return err
		}
	}

	// TODO have that container-restarting cleanup routine to handle dirty containers
	// TODO have the same cleanup routine stop dynamic containers that have been running too long

	return nil
}

func (s *ReqController) Close() error {
	s.Lock.Lock()
	defer s.Lock.Unlock()

	if err := s.cleanupContainers(); err != nil {
		return errors.Wrap(err, "Unable to cleanup containers")
	}

	return nil
}

func (s *ReqController) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	fmt.Printf("Request from %s: ", r.RemoteAddr)          // DEBUG
	fmt.Printf("%#v\n%#v\n%#v\n", r.URL, r.Host, r.Header) // DEBUG

	// In dynamic mode container(s) can be shut down, so we're starting them if that is the case.
	if s.Config.Type == DynamicController && !s.Containers[0].Started {
		s.Lock.Lock()
		if err := s.startContainers(); err != nil {
			// TODO log error
			s.Lock.Unlock()
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		s.Lock.Unlock()
	}

	s.Lock.RLock()
	defer s.Lock.RUnlock()

	chosen, err := s.getRandomContainer()
	if err != nil {
		// TODO log error
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	url := r.URL
	url.Host = fmt.Sprintf("%s:%d", chosen.IPAddr, s.Config.ContainerPort)

	proxyReq, err := http.NewRequest(r.Method, url.String(), r.Body)
	if err != nil {
		// TODO log error
		w.WriteHeader(http.StatusInternalServerError)
		return
	}

	proxyReq.Header = r.Header

	// If we'll allow non-FCGI connections, it might be good to set this (or trust it if r.RemoteAddr is a known one)
	// proxyReq.Header.Set("X-Forwarded-For", r.RemoteAddr)

	client := &http.Client{}
	res, err := client.Do(proxyReq)
	if err != nil {
		// TODO log error
		// TODO should we unlock RLock and get an actual lock before doing this?
		s.setContainerDirty(chosen.Id)
		w.WriteHeader(http.StatusBadGateway)
		return
	}
	defer res.Body.Close()

	copyHeader(w.Header(), res.Header)
	w.WriteHeader(res.StatusCode)
	io.Copy(w, res.Body)

	//w.Write([]byte("This is a FastCGI example server.\n")) // TODO actually do something
	//w.WriteHeader(200)
}

func copyHeader(dst, src http.Header) {
	for k, vv := range src {
		for _, v := range vv {
			dst.Add(k, v)
		}
	}
}

func validControllerType(c string) bool {
	for _, valid := range controllerTypes {
		if c == valid {
			return true
		}
	}

	return false
}
