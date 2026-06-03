package main

import "errors"

var errKongCommandNotImplemented = errors.New("kong command not implemented")

type BaseImageCmd struct {
	Dockerfile BaseImageDockerfileCmd `cmd:"" help:"Generate a Dockerfile for a Cog base image."`
	Build      BaseImageBuildCmd      `cmd:"" help:"Build a Cog base image."`
}

type BaseImageDockerfileCmd struct{}

func (cmd *BaseImageDockerfileCmd) Run() error {
	return errKongCommandNotImplemented
}

type BaseImageBuildCmd struct{}

func (cmd *BaseImageBuildCmd) Run() error {
	return errKongCommandNotImplemented
}

