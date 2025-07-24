# Cogpack Development Roadmap

This document outlines major work items and investigation areas for cogpack development. For current active work, see [../CLAUDE.cogpack.md](../CLAUDE.cogpack.md).

## 🚧 Active Work

### Additional Blocks
- **Apt Block**: System package installation for Debian/Ubuntu base images
- **PyTorch Block**: Framework-specific installation with GPU/CPU variants  
- **CUDA Block**: GPU runtime support with driver compatibility

### Base Image Metadata Structure
- Define metadata for resolving requirements → optimized base images
- Include venv availability to avoid conditional creation logic
- Support for cogpack-images project integration

## 🎯 Next Priorities

### 1. GPU/CUDA Support
**Challenge**: Handle CUDA version matrix, driver compatibility, multi-GPU scenarios  
**Requirements**: 
- Integration with cogpack base image metadata
- Prevent frameworks from bundling their own CUDA libraries
- CPU vs GPU accelerator detection

**Investigation needed**: 
- Audit existing Cog CUDA handling
- Design compatibility layer with base image metadata
- Framework isolation strategies

### 2. Schema.json Generation  
**Challenge**: Generate Pydantic model schema during build process  
**Requirements**:
- Available to model in source code during development
- Embedded as image label in final image
- Accessible to build system output

**Investigation needed**:
- Integration point in build pipeline
- Metadata flow from Python runtime to final image

### 3. Model Struct Architecture
**Challenge**: Replace image tag passing with centralized model metadata  
**Current**: Models identified by image tags throughout cog codebase  
**Target**: `model.Model` struct with centralized resolution from tags/IDs

**Benefits**: 
- Vastly simplified cog codebase
- Improved developer UX
- Centralized metadata management

## 🔍 Under Investigation

### Advanced Dependency Resolution Engine
**Current**: Simple version matching, some hard-coded versions, no conflict resolution  
**Challenges**:
- Metadata needed to identify dependencies in base images
- Cross-package-manager conflicts (apt vs pip)
- Dependencies added by blocks consumed by other blocks

**Next steps**: Prototype with real-world requirements.txt files

### Layer Optimization Strategies
**Current**: One layer per stage, no deduplication  
**Challenges**:
- Identify common layers across builds
- Squashing vs preservation decisions  
- BuildKit diffop availability outside containerd

**Next steps**: Analyze typical model builds for optimization opportunities

### UV Project Conversion
**Current**: Old builds use pyenv & pip install  
**Target**: All Python projects as UV projects (native or converted)
- Native UV projects: use existing pyproject.toml/uv.lock
- Legacy projects: convert requirements.txt → UV format

**Implementation**: Already completed core UV-native architecture

### Cogpack Base Images Integration
**Current**: Hardcoded base images for limited python+cuda combinations  
**Target**: Dynamic base image selection from metadata
- Metadata structure for requirement → base image resolution
- Integration with WIP cogpack-images generation repo
- Venv availability metadata to avoid conditional logic

## 🎯 Future Focus Areas

### Non-Python Stack Validation
**Purpose**: Implement basic JavaScript stack to validate architecture  
**Benefits**: 
- Proves design supports multiple languages
- Identifies Python-specific assumptions
- Validates Block/Stack interfaces

### Advanced Build Features
**Missing from old cog build system**:
- Custom build dependencies
- Complex multi-stage builds  
- Advanced caching strategies
- Build-time secret management

**Next steps**: Audit old model building code for feature gaps

## 📝 Technical Debt

### Context Conversion Efficiency
**Current**: fs.FS → temp dir → fsutil.FS conversion is inefficient  
**Impact**: Slower builds, increased disk usage  
**Solution**: Direct fs.FS to fsutil.FS adapter

### Test Coverage Gaps
**Missing coverage**:
- Integration tests for GPU builds
- Error path testing in Builder
- Multi-stage build scenarios
- Performance testing for large projects

### Plan Validation
**Missing validation**:
- ENV format validation (key=value)
- WORKDIR format (absolute path)
- CMD/ENTRYPOINT format (array of strings)
- VOLUME/EXPOSE format validation
- Cycle detection in phase dependencies

## 🏗️ Architecture Improvements

### Pending Decisions

| Topic | Options | Current State |
|-------|---------|---------------|
| **Block Ordering** | Hard-coded vs dependency graph | Hard-coded in Python stack, may need DAG for complex scenarios |
| **Remote Caching** | BuildKit cache vs custom solution | Deferred for MVP, BuildKit cache preferred |
| **Multi-architecture** | Native BuildKit vs emulation | Linux/amd64 only, expand based on demand |

### Design Questions
- **Block dependency graphs**: Do we need DAG-based ordering for complex dependencies?
- **Cross-stack interactions**: How should different language stacks interact?
- **Build reproducibility**: What level of determinism do we need?

---

**Update frequency**: Review and update this roadmap monthly or after major milestones.  
**For historical context**: See [DESIGN_DECISIONS.md](DESIGN_DECISIONS.md) and [COMPLETED_WORK.md](COMPLETED_WORK.md)