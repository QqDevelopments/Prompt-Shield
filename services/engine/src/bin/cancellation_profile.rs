//! Observe dispatch cancellation independently of ordinary timing measurements.
use anyhow::{Result, ensure};
use prompt_shield_engine::{
    detector::Detector,
    service::Engine,
    wire::{SanitizeRequest, SecurityPolicy, security_engine_server::SecurityEngine},
};
use std::{
    sync::Arc,
    time::{Duration, Instant},
};
use tonic::Request;

fn input(text: &str) -> SanitizeRequest {
    SanitizeRequest {
        request_id: "cancel-observer".into(),
        organization_id: "fixture".into(),
        text: text.into(),
        policy: Some(SecurityPolicy {
            mask_pii: true,
            block_secrets: true,
            contextual_pii: true,
            ..Default::default()
        }),
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    let engine = Arc::new(Engine::from_detector(Detector::new()?.with_ner(
        &std::env::var("ONNX_MODEL_PATH")?,
        &std::env::var("TOKENIZER_CONFIG_PATH")?,
    )?));
    // 510 ordinary words plus special tokens reach the model's 512-token limit.
    let text = vec!["word"; 510].join(" ");
    engine
        .inspect_and_sanitize(Request::new(input(&text)))
        .await?;
    const ITERATIONS: usize = 20;
    const MAX_ATTEMPTS: usize = ITERATIONS * 3;
    let mut samples = Vec::with_capacity(ITERATIONS);
    let mut attempts = 0;
    let mut observation_misses = 0;
    while samples.len() < ITERATIONS && attempts < MAX_ATTEMPTS {
        attempts += 1;
        let mut handles = Vec::new();
        for _ in 0..4 {
            let engine = Arc::clone(&engine);
            let request = input(&text);
            handles.push(tokio::spawn(async move {
                engine.inspect_and_sanitize(Request::new(request)).await
            }));
        }
        let wait = Instant::now();
        while engine.evaluation_ner_inflight() == 0
            && wait.elapsed() < Duration::from_secs(2)
            && handles.iter().any(|handle| !handle.is_finished())
        {
            tokio::task::yield_now().await;
        }
        let ner_before = engine.evaluation_ner_inflight();
        let before = engine.evaluation_inflight();
        if ner_before == 0 {
            observation_misses += 1;
            for handle in handles {
                match handle.await {
                    Ok(Ok(_)) => (),
                    Ok(Err(error)) if error.code() == tonic::Code::ResourceExhausted => (),
                    result => anyhow::bail!("unexpected unobserved dispatch result: {result:?}"),
                }
            }
            continue;
        }
        let canceled_at = Instant::now();
        for handle in &handles {
            handle.abort();
        }
        let mut aborted = 0;
        for handle in handles {
            match handle.await {
                Err(error) if error.is_cancelled() => aborted += 1,
                Ok(Ok(_)) => (),
                Ok(Err(error)) if error.code() == tonic::Code::ResourceExhausted => (),
                result => anyhow::bail!("unexpected dispatch result: {result:?}"),
            }
        }
        let dispatch_return_ms = canceled_at.elapsed().as_secs_f64() * 1000.0;
        let ner_after = engine.evaluation_ner_inflight();
        let after = engine.evaluation_inflight();
        while engine.evaluation_inflight() > 0 && canceled_at.elapsed() < Duration::from_secs(10) {
            tokio::time::sleep(Duration::from_millis(1)).await;
        }
        ensure!(engine.evaluation_inflight() == 0, "inference did not drain");
        let drain_ms = canceled_at.elapsed().as_secs_f64() * 1000.0;
        engine
            .inspect_and_sanitize(Request::new(input("The meeting starts tomorrow.")))
            .await?;
        samples.push(
            serde_json::json!({"inflight_before_abort":before,"inflight_after_abort":after,
            "ner_inflight_before_abort":ner_before,"ner_inflight_after_abort":ner_after,
            "aborted_dispatches":aborted,"dispatch_return_ms":dispatch_return_ms,
            "all_work_drained_ms":drain_ms,"recovery_passed":true}),
        );
    }
    ensure!(
        samples.len() == ITERATIONS,
        "observed only {} in-flight cancellation rounds after {attempts} attempts",
        samples.len()
    );
    println!(
        "{}",
        serde_json::json!({"schema_version":1,"scope":"real public Rust service dispatch; HTTP/gRPC transport cancellation measured separately",
        "model_tokens":512,"concurrency":4,"iterations":ITERATIONS,"attempts":attempts,
        "observation_misses":observation_misses,"samples":samples,
        "note":"Dispatch abort terminates request-specific RunOptions; capacity remains occupied until native work exits."})
    );
    Ok(())
}
