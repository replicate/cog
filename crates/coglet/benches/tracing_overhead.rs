use std::hint::black_box;

use criterion::{Criterion, criterion_group, criterion_main};
use opentelemetry::trace::TracerProvider as _;
use opentelemetry_sdk::trace::{InMemorySpanExporter, Sampler, SdkTracerProvider};
use tracing::Dispatch;
use tracing_subscriber::layer::SubscriberExt as _;

fn dispatch(sampler: Sampler) -> (Dispatch, SdkTracerProvider) {
    let provider = SdkTracerProvider::builder()
        .with_sampler(sampler)
        .with_simple_exporter(InMemorySpanExporter::default())
        .build();
    let tracer = provider.tracer("benchmark");
    let subscriber =
        tracing_subscriber::registry().with(tracing_opentelemetry::layer().with_tracer(tracer));
    (Dispatch::new(subscriber), provider)
}

fn tracing_overhead(c: &mut Criterion) {
    let mut group = c.benchmark_group("tracing_overhead");
    group.bench_function("disabled", |b| {
        b.iter(|| black_box(tracing::Span::none()));
    });
    let (unsampled, unsampled_provider) = dispatch(Sampler::AlwaysOff);
    group.bench_function("enabled_unsampled", |b| {
        b.iter(|| {
            tracing::dispatcher::with_default(&unsampled, || {
                black_box(tracing::info_span!("prediction"));
            })
        });
    });
    let (sampled, sampled_provider) = dispatch(Sampler::AlwaysOn);
    group.bench_function("enabled_sampled", |b| {
        b.iter(|| {
            tracing::dispatcher::with_default(&sampled, || {
                black_box(tracing::info_span!("prediction"));
            })
        });
    });
    group.finish();
    let _ = unsampled_provider.shutdown();
    let _ = sampled_provider.shutdown();
}

criterion_group!(benches, tracing_overhead);
criterion_main!(benches);
