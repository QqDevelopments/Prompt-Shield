#![cfg(feature = "evaluation")]
use prompt_shield_engine::{
    detector::Detector,
    ner::NerConfig,
    service::Engine,
    wire::{security_engine_server::SecurityEngine, *},
};
use std::{sync::Arc, time::Duration};
use tonic::Request;

fn input(id: usize, text: &str, contextual: bool) -> SanitizeRequest {
    SanitizeRequest {
        request_id: format!("pool-{id}"),
        organization_id: "pool-fixture".into(),
        text: text.into(),
        policy: Some(SecurityPolicy {
            mask_pii: true,
            contextual_pii: contextual,
            ..Default::default()
        }),
    }
}

#[tokio::test]
#[ignore = "requires pinned model"]
async fn canceled_queue_keeps_rules_available_and_recovers_without_cross_request_data()
-> Result<(), Box<dyn std::error::Error>> {
    for sessions in [1, 2, 4] {
        let engine = Arc::new(Engine::from_detector(Detector::new()?.with_ner_config(
            "../../models/general-ner/model.onnx",
            "../../models/general-ner/tokenizer.json",
            NerConfig {
                sessions,
                intra_threads: 1,
                spinning: false,
            },
        )?));
        let long = vec!["word"; 510].join(" ");
        for round in 0..20 {
            let mut tasks = Vec::new();
            for id in 0..4 {
                let engine = Arc::clone(&engine);
                let request = input(id, &long, true);
                tasks.push(tokio::spawn(async move {
                    engine.inspect_and_sanitize(Request::new(request)).await
                }));
            }
            tokio::time::timeout(Duration::from_secs(10), async {
                while engine.evaluation_ner_inflight() < 4 {
                    tokio::task::yield_now().await;
                }
            })
            .await?;
            if sessions == 1 {
                let rejected = engine
                    .inspect_and_sanitize(Request::new(input(9, &long, true)))
                    .await;
                assert_eq!(
                    rejected.err().map(|error| error.code()),
                    Some(tonic::Code::ResourceExhausted)
                );
            }
            // Real fail-fast and rule-only dispatches must work while NER is full.
            let blocked = engine
                .inspect_and_sanitize(Request::new(input(10, "sk-abcdefghijklmnopqrstuvwx", true)))
                .await?
                .into_inner();
            assert!(blocked.is_blocked);
            let rule = engine
                .inspect_and_sanitize(Request::new(input(11, "Email alice@example.com", false)))
                .await?
                .into_inner();
            assert_eq!(rule.entity_types, ["EMAIL"]);
            for task in &tasks {
                task.abort();
            }
            for task in tasks {
                match task.await {
                    Ok(Ok(_)) => (),
                    Err(error) if error.is_cancelled() => (),
                    result => return Err(format!("unexpected dispatch outcome: {result:?}").into()),
                }
            }
            tokio::time::timeout(Duration::from_secs(10), async {
                while engine.evaluation_inflight() > 0 {
                    tokio::task::yield_now().await;
                }
            })
            .await?;
            let mut tasks = Vec::new();
            for (id, text) in [
                "John Smith works at Microsoft in London.",
                "Alice Johnson visited Paris.",
                "Email bob@example.com",
                "The meeting starts tomorrow.",
            ]
            .into_iter()
            .enumerate()
            {
                let engine = Arc::clone(&engine);
                tasks.push(tokio::spawn(async move {
                    let request = input(100 + round * 4 + id, text, true);
                    let request_id = request.request_id.clone();
                    let result = engine
                        .inspect_and_sanitize(Request::new(request))
                        .await?
                        .into_inner();
                    let restored = engine
                        .de_anonymize(Request::new(DeAnonymizeRequest {
                            request_id,
                            organization_id: "pool-fixture".into(),
                            text: result.sanitized_text,
                            mappings: result.mappings,
                        }))
                        .await?
                        .into_inner();
                    assert_eq!(restored.text, text);
                    Ok::<_, tonic::Status>(())
                }));
            }
            for task in tasks {
                task.await??;
            }
            assert_eq!(engine.evaluation_inflight(), 0);
        }
    }
    Ok(())
}

#[test]
fn configuration_enforces_the_eight_thread_budget() {
    for sessions in [0, 1, 2, 3, 4, 5, usize::MAX] {
        for intra_threads in [0, 1, 2, 3, 4, 5, usize::MAX] {
            let valid = [1, 2, 4].contains(&sessions)
                && [1, 2, 4].contains(&intra_threads)
                && sessions.saturating_mul(intra_threads) <= 8;
            assert_eq!(
                NerConfig {
                    sessions,
                    intra_threads,
                    spinning: false
                }
                .validate()
                .is_ok(),
                valid
            );
        }
    }
}
