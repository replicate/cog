# Cog Architecture Overview

Cog packages machine learning models into production-ready OCI images.

## The Big Picture

```mermaid
flowchart LR
    subgraph input["What you write"]
        model["Model Code<br/>+ cog.yaml"]
    end
    
    subgraph cog["Cog"]
        cli["CLI"]
        sdk["Python SDK"]
    end
    
    subgraph output["What you get"]
        image["Container Image"]
        api["HTTP API"]
    end
    
    model --> cli
    cli -->|"builds"| image
    image -->|"runs"| sdk
    sdk -->|"serves"| api
```

## Components

### Model Source

What the model author provides: `cog.yaml` for environment config, a Predictor class with `setup()` and `predict()` methods, and optionally model weights.

**Deep dive**: [Model Source](./01-model-source.md)

---

### Schema

An OpenAPI specification generated from the predictor's type hints. Describes what inputs the model accepts and what outputs it produces.

**Deep dive**: [Schema](./02-schema.md)

---

### Prediction API

The HTTP interface for running predictions. A fixed envelope format (`PredictionRequest`/`PredictionResponse`) wraps model-specific inputs and outputs.

**Deep dive**: [Prediction API](./03-prediction-api.md)

---

### Container Runtime

The Python runtime that runs inside the container: an HTTP server, worker process isolation, and prediction execution.

**Deep dive**: [Container Runtime](./04-container-runtime.md)

---

### Build System

Transforms `cog.yaml` and user code into a Docker image with the right Python version, CUDA libraries, and dependencies.

**Deep dive**: [Build System](./05-build-system.md)

---

### CLI

The command-line tool for building, testing, and deploying models.

**Deep dive**: [CLI](./06-cli.md)

---

## How It Fits Together

```mermaid
flowchart TB
    subgraph source["Model Source"]
        yaml["cog.yaml"]
        code["predict.py"]
        weights["weights"]
    end
    
    subgraph build["Build Time"]
        config["Config Parser"]
        generator["Dockerfile Generator"]
        schema_gen["Schema Generator"]
    end
    
    subgraph image["Container Image"]
        layers["Base + Deps + Code"]
        schema["OpenAPI Schema<br/>(label)"]
    end
    
    subgraph runtime["Runtime"]
        server["HTTP Server"]
        worker["Worker Process"]
        predictor["Predictor"]
    end
    
    yaml --> config
    config --> generator
    generator --> layers
    code --> layers
    weights --> layers
    
    layers --> schema_gen
    schema_gen --> schema
    
    image --> server
    server --> worker
    worker --> predictor
```

## Terminology

| Term | Meaning |
|------|---------|
| **Predictor** | User's model class with `setup()` and `predict()` methods |
| **Schema** | OpenAPI spec describing the model's input/output interface |
| **Envelope** | Fixed request/response structure wrapping model-specific data |
| **Worker** | Isolated subprocess running user code |
| **Setup** | One-time model initialization at container start |

## Reading Order

For understanding Cog's architecture, we recommend reading in this order:

1. [Model Source](./01-model-source.md) - What users write
2. [Schema](./02-schema.md) - How the interface is described
3. [Prediction API](./03-prediction-api.md) - How requests and responses work
4. [Container Runtime](./04-container-runtime.md) - How it runs
5. [Build System](./05-build-system.md) - How images are built
6. [CLI](./06-cli.md) - How users interact with it all
