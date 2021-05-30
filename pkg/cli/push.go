package cli

import (
	"fmt"
	"sync"

	"github.com/spf13/cobra"

	"github.com/replicate/cog/pkg/client"
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

type archLogEntry struct {
	entry *client.LogEntry
	arch  string
}

func newPushCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "push",
		Short: "Push version",
		RunE:  push,
		Args:  cobra.NoArgs,
	}
	addModelFlag(cmd)
	addProjectDirFlag(cmd)

	cmd.Flags().Bool("log", false, "Follow image build logs after successful push")

	return cmd
}

func push(cmd *cobra.Command, args []string) error {
	log, err := cmd.Flags().GetBool("log")
	if err != nil {
		return err
	}

	model, err := getModel()
	if err != nil {
		return err
	}

	config, projectDir, err := getConfig()
	if err != nil {
		return err
	}

	if config.Model == "" {
		return fmt.Errorf("To push a model, you must set the 'model' option in cog.yaml.")
	}

	console.Infof("Uploading %s to %s", projectDir, model)

	cli := client.NewClient()
	version, err := cli.UploadVersion(model, projectDir)
	if err != nil {
		return err
	}

	fmt.Printf("Successfully uploaded version %s\n", version.ID)

	if log {
		return pushLog(model, version)
	}

	return nil
}

func pushLog(model *model.Model, version *model.Version) error {
	c := client.NewClient()

	logChans := map[string]chan *client.LogEntry{}
	for _, arch := range version.Config.Environment.Architectures {
		logChan, err := c.GetBuildLogs(model, version.BuildIDs[arch], true)
		if err != nil {
			return err
		}
		logChans[arch] = logChan
	}

	for archEntry := range mergeLogs(logChans) {
		prefix := ""
		if len(logChans) > 1 {
			prefix = fmt.Sprintf("[%s] ", archEntry.arch)
		}
		outputLogEntry(archEntry.entry, prefix)
	}
	return nil
}

func mergeLogs(channelMap map[string]chan *client.LogEntry) <-chan *archLogEntry {
	out := make(chan *archLogEntry)
	var wg sync.WaitGroup
	wg.Add(len(channelMap))
	for arch, c := range channelMap {
		go func(arch string, c <-chan *client.LogEntry) {
			for entry := range c {
				out <- &archLogEntry{
					arch:  arch,
					entry: entry,
				}
			}
			wg.Done()
		}(arch, c)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
