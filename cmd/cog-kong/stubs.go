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

type WeightsCmd struct {
	Import WeightsImportCmd `cmd:"" help:"Import model weights."`
	Pull   WeightsPullCmd   `cmd:"" help:"Pull model weights."`
	Status WeightsStatusCmd `cmd:"" help:"Show model weight status."`
}

type WeightsImportCmd struct{}

func (cmd *WeightsImportCmd) Run() error {
	return errKongCommandNotImplemented
}

type WeightsPullCmd struct{}

func (cmd *WeightsPullCmd) Run() error {
	return errKongCommandNotImplemented
}

type WeightsStatusCmd struct{}

func (cmd *WeightsStatusCmd) Run() error {
	return errKongCommandNotImplemented
}
