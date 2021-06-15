package database

import (
	"github.com/replicate/cog/pkg/model"
	"github.com/replicate/cog/pkg/util/console"
)

type migrator struct {
	sourceDB      Database
	destinationDB Database
	dryRun        bool
}

func Migrate(sourceDB Database, destinationDB Database, dryRun bool) error {
	migrator := &migrator{
		sourceDB:      sourceDB,
		destinationDB: destinationDB,
		dryRun:        dryRun,
	}
	return migrator.migrate()
}

func (m *migrator) migrate() error {
	userModels, err := m.sourceDB.ListUserModels()
	if err != nil {
		return err
	}
	for _, userModel := range userModels {
		user := userModel.Username
		name := userModel.ModelName
		versions, err := m.sourceDB.ListVersions(user, name)
		if err != nil {
			return err
		}
		for _, version := range versions {
			if err := m.migrateVersion(user, name, version); err != nil {
				return err
			}
		}
	}
	return nil
}
func (m *migrator) migrateVersion(user string, name string, version *model.Version) error {
	existing, err := m.destinationDB.GetVersion(user, name, version.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		console.Infof("Version already exists: %s/%s:%s", user, name, version.ID)
		return nil
	}

	console.Infof("Inserting version %s/%s:%s", user, name, version.ID)
	if !m.dryRun {
		if err := m.destinationDB.InsertVersion(user, name, version.ID, version); err != nil {
			return err
		}
	}

	for arch, buildID := range version.BuildIDs {
		if err := m.migrateImage(user, name, version.ID, arch); err != nil {
			return err
		}
		if err := m.migrateBuildLogs(user, name, buildID); err != nil {
			return err
		}
	}

	return nil
}

func (m *migrator) migrateImage(user, name, id, arch string) error {
	image, err := m.sourceDB.GetImage(user, name, id, arch)
	if err != nil {
		return err
	}
	console.Infof("Inserting image %s/%s:%s, Arch: %s", user, name, id, arch)
	if !m.dryRun {
		if err := m.destinationDB.InsertImage(user, name, id, arch, image); err != nil {
			return err
		}
	}
	return nil
}

func (m *migrator) migrateBuildLogs(user, name, buildID string) error {
	buildLogs, err := m.sourceDB.GetBuildLogs(user, name, buildID, false)
	if err != nil {
		return err
	}
	console.Infof("Inserting build logs %s/%s, ID: %s", user, name, buildID)
	if !m.dryRun {
		for line := range buildLogs {
			if err := m.destinationDB.AddBuildLogLine(user, name, buildID, line.Line, line.Level, line.Timestamp); err != nil {
				return err
			}
		}
		// assume the build is completed
		// TODO(andreas): maybe handle this better?
		if err := m.destinationDB.FinalizeBuildLog(user, name, buildID); err != nil {
			return err
		}
	}
	return nil
}
