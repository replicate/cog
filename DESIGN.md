# Design

## Background

Cog came from Andreas's experience at Spotify and Ben's experience at Docker.

At Spotify, Andreas noticed a cluster of related problems:

- **It was hard to run open-source machine learning models.** All the advances in machine learning were locked up inside prose in PDFs, scraps of code on GitHub, weights on Google Drive (if you were lucky!). If you wanted to build upon this research, or apply it to real-world problems, you had to implement it all from scratch.
- **It was hard to deploy machine learning models to production.** Andreas was the only person on the research team who was also an infrastructure engineer. Typically a researcher would have to sit down with Andreas to decide on an API, get a server written, package up dependencies, battle CUDA, get it running efficiently, get it deployed on the cluster, and so on and so forth. It would take weeks to get something running in production.

Ben connected this back to his experience at Docker. What Docker did was define a standard box that software could go in. You could put any kind of server software in there – Python, Java, Ruby on Rails, whatever – and you could then know that you could run it on your local machine or on any cloud, as long as it supported Docker. We wanted to do the same thing for machine learning.

## Vision

We want Cog to be a standard artifact for what a model is and how that model is run.

(More detail...)

## Design principles

There are a few things driving Cog's design:

- **Reproducible artifact**: When you've put your model in Cog, it'll run anywhere, and _keep_ on running. This is why it's a Docker image, with all of the model's dependencies. Docker images have a content-addressable SHA256 ID, which is the identifier for that model's behavior, byte-for-byte. 
- **Weights inside the image**: We encourage users to put model weights in images. If the weights are on cloud storage somewhere, then they might change or disappear, and the image will produce different results. There's nothing magical about Docker images – they're just a bundle of files. Docker moves around that bundle of files quite slowly, though, but we can optimize that process so it's as fast as reading weights directly from blob storage, or wherever.
- **Models are just functions**: Models can be lots of things. We are of the opinion that [machine learning is just software](https://replicate.com/blog/machine-learning-needs-better-tools), and a model is just a function. It often needs to be attached to a GPU, but apart from that it's just a normal function that has some input and some output. This is the core difference between Docker's abstraction and Cog's abstraction: Docker packages up an executable, whereas Cog packages up a _function_.
- **Standard interface**: When you run the Docker container, it serves an HTTP server, that is a standard API for running that function. You can think of it like a remote procedure call.
- **Self-describing artifact**: A Cog model has it's schema (or type signature, if you're thinking of it as a function) attached to the image as a label. This means systems that work with Cog models can know what the model is and what requests to send to it. This is what powers the forms on Replicate, for example.
- **Not just the model**: Before Cog, the typical standard packaging formats for machine learning models were at the network level. A way of taking a Tensorflow or PyTorch network and packaging up in a way that would run on lots of different types of accelerators. Things like [ONNX](https://onnx.ai/) or [TVM's IR](https://tvm.apache.org/). We realized that "models" are not just the network, but they are also pre- and post-processing, and are so diverse and the field is so fast-moving that you can't possible squeeze it into some high-level abstraction. It just needs to be code running on a computer.
- **It's just Docker**: Cog models need to run anywhere, and they'll only run anywhere if it's vanilla Docker. We might optimize how Docker images get shipped around to make it faster, but we're not going to invent our own image format.
- **The API is for software developers**: In the olden days, you have to pass tensors to TFServing and know how to generate a tensor from a JPEG. Cog's API intentionally just speaks JSON, strings, files, etc. It's intended to be the interface between the software developer and the ML engineer. Sort of like Docker was intended to be the interface between the software developer and the infrastructure engineer.
- **Cog is the APIs and interfaces, not just the software**: The most important thing about Cog is that it defines a standard for what a model is and how to run it. It doesn't necessarily need to involve Cog the piece of software itself. For example, Replicate could serve a model from OpenAI with a Cog API and schema, but it's not packaged or running with Cog under the hood at all – it's just calling the OpenAI API directly.
