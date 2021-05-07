package shell

import (
	"bufio"
	"io"
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
