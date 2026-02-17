package dockercontext

import "path/filepath"

const StandardBuildDirectory = "."

const ContextBuildDir = "context"
const AptBuildContextName = "apt"
const RequirementsBuildContextName = "requirements"
const SrcBuildContextName = "src"

var SrcBuildDir = filepath.Join(ContextBuildDir, "src")
var AptBuildDir = filepath.Join(ContextBuildDir, "apt")
var RequirementsBuildDir = filepath.Join(ContextBuildDir, "requirements")
