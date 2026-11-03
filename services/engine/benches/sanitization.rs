use criterion::{Criterion, criterion_group, criterion_main};
use prompt_shield_engine::{detector::Detector, wire::SecurityPolicy};
use std::hint::black_box;
fn bench(c: &mut Criterion) {
    let detector = Detector::new().expect("benchmark detector initialization");
    let policy = SecurityPolicy {
        mask_pii: true,
        block_secrets: true,
        ..Default::default()
    };
    for (name, text) in [
        ("clean", "Explain a rainbow."),
        ("masked", "Email alice@example.com"),
        ("blocked", "sk-abcdefghijklmnopqrstuvwx"),
    ] {
        c.bench_function(name, |b| {
            b.iter(|| detector.inspect(black_box(text), black_box(&policy)))
        });
    }
    #[cfg(feature = "ner")]
    if std::env::var("NER_BENCH").is_ok_and(|value| value == "true") {
        let detector = Detector::new()
            .expect("benchmark detector")
            .with_ner(
                "../../models/general-ner/model.onnx",
                "../../models/general-ner/tokenizer.json",
            )
            .expect("pinned model required");
        let policy = SecurityPolicy {
            mask_pii: true,
            block_secrets: true,
            contextual_pii: true,
            ..Default::default()
        };
        c.bench_function("ner_short", |b| {
            b.iter(|| {
                detector.inspect(
                    black_box("John Smith works at Microsoft in London."),
                    black_box(&policy),
                )
            })
        });
    }
}
criterion_group!(benches, bench);
criterion_main!(benches);
