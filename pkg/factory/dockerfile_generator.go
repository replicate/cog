package factory

import (
	"fmt"
	"strings"
)

// GenerateDockerfile renders a simple multi-stage Dockerfile that uses the
// DevTag for the build stage and RunTag for the runtime stage. The output is
// intentionally minimal for the first iteration.
func GenerateDockerfile(spec *BuildSpec) (string, error) {
	if spec == nil || spec.BaseImage == nil {
		return "", fmt.Errorf("invalid build spec – base image missing")
	}

	// We require DevTag and RunTag in the CSV. Fallbacks if not present.
	buildImage := spec.BaseImage.DevTag
	if buildImage == "" {
		buildImage = spec.BaseImage.RunTag
	}
	runtimeImage := spec.BaseImage.RunTag
	if runtimeImage == "" {
		runtimeImage = spec.BaseImage.DevTag
	}

	var sb strings.Builder

	sb.WriteString("# syntax=docker/dockerfile:1\n")

	// Builder stage
	sb.WriteString("FROM " + buildImage + " AS build\n")
	sb.WriteString("WORKDIR /src\n")

	// Copy embedded Cog wheel and install it alongside pydantic
	wheel := spec.CogWheelFilename
	if wheel == "" {
		wheel = "cog-wheel.whl" // fallback, should not happen
	}
	fmt.Fprintf(&sb, "ENV UV_LINK_MODE=copy\n")
	fmt.Fprintf(&sb, "ENV UV_PYTHON_INSTALL_DIR=/python\n")
	fmt.Fprintf(&sb, "ENV UV_COMPILE_BYTECODE=1\n")
	sb.WriteString("COPY .cog/" + wheel + " /tmp/" + wheel + "\n")
	sb.WriteString("RUN uv pip install --python /venv/bin/python --no-cache-dir /tmp/" + wheel + " 'pydantic>=1.9,<3' && rm /tmp/" + wheel + "\n")

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
	sb.WriteString("FROM " + runtimeImage + " AS runtime\n")
	sb.WriteString("WORKDIR /src\n")
	sb.WriteString("COPY --from=build /venv /venv\n")

	fmt.Fprintln(&sb, "EXPOSE 5000/tcp")
	sb.WriteString("COPY . .\n")
	// sb.WriteString("ENV PATH=/venv/bin:$PATH\n")
	// Basic entrypoint – the existing label injection step will eventually
	// adjust this, but we add a reasonable default.
	// fmt.Fprintln(&sb, "ENTRYPOINT /usr/bin/tini --")
	sb.WriteString("CMD [\"python\", \"-m\", \"cog.server.http\"]\n")

	return sb.String(), nil
}
