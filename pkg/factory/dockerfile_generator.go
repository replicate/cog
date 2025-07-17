//go:build ignore

package factory

import (
	"bytes"
	"fmt"
	"io"
	"maps"
	"path"
	"slices"
	"strings"
)

// GenerateDockerfile renders a simple multi-stage Dockerfile that uses the
// DevTag for the build stage and RunTag for the runtime stage. The output is
// intentionally minimal for the first iteration.
func GenerateDockerfile(spec *BuildSpec) (string, error) {
	if spec == nil || spec.BaseImage == nil {
		return "", fmt.Errorf("invalid build spec – base image missing")
	}

	// aptPackages := &AptPackages{
	// 	Packages:   spec.SystemPackages,
	// 	CacheMount: true,
	// }

	// We require DevTag and RunTag in the CSV. Fallbacks if not present.
	buildImage := spec.BaseImage.DevTag
	runtimeImage := spec.BaseImage.RunTag

	var sb strings.Builder

	// we're using buildkit features only recently added to the dockerfile frontend, so keep this pinned to latest.
	// don't use looser tags (like "1") since an older version may be cached which won't satisfy the min version needed
	sb.WriteString("# syntax=docker/dockerfile:1.17-labs\n")

	// Builder stage
	buildStage := newStage("build", buildImage)
	buildStage.Workdir("/src")

	// aptPackages.Generate(buildStage)

	// Copy embedded Cog wheel and install it alongside pydantic
	wheel := spec.CogWheelFilename
	if wheel == "" {
		wheel = "cog-wheel.whl" // fallback, should not happen
	}
	buildStage.Env(map[string]string{
		"UV_LINK_MODE":          "copy",
		"UV_PYTHON_INSTALL_DIR": "/python",
		"UV_COMPILE_BYTECODE":   "1",
	})

	buildStage.Copy(copyOpts{
		src: []string{path.Join(".cog", wheel)},
		dst: path.Join("/tmp", wheel),
	})

	buildStage.RunCommand("uv pip install --python /venv/bin/python --no-cache-dir /tmp/" + wheel + " 'pydantic>=1.9,<3' && rm /tmp/" + wheel)

	if err := spec.PythonRequirements.Apply(buildStage); err != nil {
		return "", fmt.Errorf("failed to apply python requirements: %w", err)
	}

	// Python dependencies
	if spec.HasRequirements {
		// We assume the base image already has uv and python installed at /python.
		if spec.RequirementsFile != "" && spec.RequirementsFile != "requirements.txt" {
			// Copy the specified file to ./requirements.txt inside image for convenience
			sb.WriteString("COPY " + spec.RequirementsFile + " ./requirements.txt\n")
		} else {
			// Default: attempt to copy requirements.txt if present; don't fail if absent.
			sb.WriteString("COPY requirements.txt ./ || true\n")
		}
		sb.WriteString("RUN if [ -f requirements.txt ]; then uv pip install -r requirements.txt --venv /venv; fi\n")
	}

	// Runtime stage
	runtimeStage := newStage("runtime", runtimeImage)
	runtimeStage.Workdir("/src")

	// aptPackages.Generate(runtimeStage)

	runtimeStage.Copy(copyOpts{
		src:  []string{"/venv"},
		dst:  "/venv",
		from: "build",
		link: true,
	})

	runtimeStage.Expose("5000", "tcp")

	runtimeStage.Copy(copyOpts{
		src:     []string{"."},
		dst:     ".",
		exclude: []string{".cog"},
	})

	// sb.WriteString("COPY . .\n")
	// sb.WriteString("ENV PATH=/venv/bin:$PATH\n")
	// Basic entrypoint – the existing label injection step will eventually
	// adjust this, but we add a reasonable default.
	// fmt.Fprintln(&sb, "ENTRYPOINT /usr/bin/tini --")
	// sb.WriteString("CMD [\"python\", \"-m\", \"cog.server.http\"]\n")
	runtimeStage.Cmd("python", "-m", "cog.server.http")

	io.Copy(&sb, &buildStage.buf)
	io.Copy(&sb, &runtimeStage.buf)

	return sb.String(), nil
}

// stableSlice returns a sorted copy of the input slice with duplicate
// entries removed.
func stableSlice(in []string) []string {
	if len(in) == 0 {
		return in
	}

	out := slices.Clone(in)
	slices.Sort(out)
	return slices.Compact(out)
}

func newStage(name, from string) *stage {
	s := &stage{
		name: name,
		from: from,
	}

	fmt.Fprintf(&s.buf, "FROM %s AS %s\n", from, name)

	return s
}

type stage struct {
	name string
	from string
	buf  bytes.Buffer
}

func (s *stage) Workdir(dir string) {
	fmt.Fprintf(&s.buf, "WORKDIR %s\n", dir)
}

type runOpts struct {
	mounts   []string
	commands []string
}

func (s *stage) Run(runOpts runOpts) {
	fmt.Fprint(&s.buf, "RUN")
	if len(runOpts.mounts) > 0 {
		fmt.Fprintf(&s.buf, " %s", strings.Join(runOpts.mounts, " "))
	}
	fmt.Fprint(&s.buf, " <<EOF\n")
	for _, cmd := range runOpts.commands {
		fmt.Fprintln(&s.buf, "  "+cmd)
	}
	fmt.Fprintln(&s.buf, "EOF")
}

func (s *stage) RunCommand(cmd string) {
	s.Run(runOpts{
		commands: []string{cmd},
	})
}

type copyOpts struct {
	src     []string
	dst     string
	from    string
	chown   string
	chmod   string
	link    bool
	exclude []string
}

func (s *stage) Copy(copyOpts copyOpts) {
	fmt.Fprint(&s.buf, "COPY")
	if copyOpts.from != "" {
		fmt.Fprintf(&s.buf, " --from=%s", copyOpts.from)
	}
	if copyOpts.chown != "" {
		fmt.Fprintf(&s.buf, " --chown=%s", copyOpts.chown)
	}
	if copyOpts.chmod != "" {
		fmt.Fprintf(&s.buf, " --chmod=%s", copyOpts.chmod)
	}
	if copyOpts.link {
		fmt.Fprintf(&s.buf, " --link")
	}
	if len(copyOpts.exclude) > 0 {
		for _, v := range stableSlice(copyOpts.exclude) {
			fmt.Fprintf(&s.buf, " --exclude=%s", v)
		}
	}
	for _, v := range stableSlice(copyOpts.src) {
		fmt.Fprintf(&s.buf, " %s", v)
	}
	fmt.Fprintf(&s.buf, " %s\n", copyOpts.dst)
}

func (s *stage) Expose(port, proto string) {
	fmt.Fprintf(&s.buf, "EXPOSE %s/%s\n", port, proto)
}

func (s *stage) Cmd(cmd ...string) {
	fmt.Fprint(&s.buf, "CMD [")
	for i, v := range cmd {
		if i > 0 {
			fmt.Fprint(&s.buf, ", ")
		}
		fmt.Fprintf(&s.buf, "%q", v)
	}
	fmt.Fprint(&s.buf, "]\n")
}

func (s *stage) Env(envs map[string]string) {
	for _, k := range slices.Sorted(maps.Keys(envs)) {
		fmt.Fprintf(&s.buf, "ENV %s=%q\n", k, envs[k])
	}
}
