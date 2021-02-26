package server

import (
	"bufio"
	"io"
	"strings"

	log "github.com/sirupsen/logrus"
)

type PipeFunc func() (io.ReadCloser, error)
type LogFunc func(args ...interface{})

func PipeTo(pf PipeFunc, lf LogFunc) (done chan struct{}, err error) {
	done = make(chan struct{})

	pipe, err := pf()
	if err != nil {
		return nil, err
	}
	scanner := bufio.NewScanner(pipe)
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			lf(line)
		}
		done <- struct{}{}
	}()

	return done, nil
}

func pipeToWithDockerChecks(pf PipeFunc, lf LogFunc) (done chan struct{}, err error) {
	return PipeTo(pf, func(args ...interface{}) {
		line := args[0].(string)
		if strings.Contains(line, "Cannot connect to the Docker daemon") {
			log.Fatal("Docker does not appear to be running; please start Docker and try again")
		}
		if strings.Contains(line, "failed to dial gRPC: unable to upgrade to h2c, received 502") {
			log.Fatal("Your Docker version appears to be out out date; please upgrade Docker to the latest version and try again")
		}
		lf(line)
	})
}
