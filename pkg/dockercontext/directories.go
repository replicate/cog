package dockercontext

import "path/filepath"

const StandardBuildDirectory = "."

const ContextBuildDir = "context"
const AptBuildContextName = "apt"
const MonobaseBuildContextName = "monobase"
const RequirementsBuildContextName = "requirements"
const SrcBuildContextName = "src"

var SrcBuildDir = filepath.Join(ContextBuildDir, "src")
var AptBuildDir = filepath.Join(ContextBuildDir, "apt")
var MonobaseBuildDir = filepath.Join(ContextBuildDir, "monobase")
var RequirementsBuildDir = filepath.Join(ContextBuildDir, "requirements")
