package fpm

import (
	"fmt"
	"github.com/pkg/errors"
	"net"
	"net/http/fcgi"
	"os"
	"os/user"
	"strconv"
)

func NewSocketFCGIServer(config ControllerConfig, path, owner, group string) error {
	usr, err := user.Lookup(owner)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to find user %s", owner))
	}
	userId, err := strconv.Atoi(usr.Uid)
	if err != nil {
		return errors.Wrap(err, "User ID is not a number")
	}

	grp, err := user.LookupGroup(group)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to find group %s", group))
	}
	groupId, err := strconv.Atoi(grp.Gid)
	if err != nil {
		return errors.Wrap(err, "Group ID is not a number")
	}

	l, err := net.Listen("unix", path)
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to listen on %s", path))
	}

	defer l.Close()
	defer os.Remove(path)

	if err := os.Chown(path, userId, groupId); err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to change socker file ownership to %s:%s", owner, group))
	}

	h, err := NewReqController(config)
	if err != nil {
		return errors.Wrap(err, "Unable to setup request controller")
	}
	if err = h.Init(); err != nil {
		return errors.Wrap(err, "Unable to initialize request controller")
	}

	fcgi.Serve(l, &h)
	// TODO make sure socket is closed and removed and controller is shut down with Close() after interrupted

	return nil
}

func NewTCPFCGIServer(config ControllerConfig, ipAddr string, port int) error {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", ipAddr, port))
	if err != nil {
		return errors.Wrap(err, fmt.Sprintf("Unable to listen on %s:%d", ipAddr, port))
	}

	h, err := NewReqController(config)
	if err != nil {
		return errors.Wrap(err, "Unable to setup request controller")
	}
	if err = h.Init(); err != nil {
		return errors.Wrap(err, "Unable to initialize request controller")
	}

	fcgi.Serve(l, &h)
	// TODO make sure controller is shut down with Close() after interrupted

	return nil
}
