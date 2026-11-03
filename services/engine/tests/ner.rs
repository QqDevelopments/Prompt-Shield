#![cfg(feature = "ner")]
use prompt_shield_engine::{
    detector::Detector,
    service::Engine,
    wire::{security_engine_server::SecurityEngine, *},
};
use tonic::Request;

#[tokio::test]
#[ignore = "requires pinned model: node --experimental-strip-types scripts/fetch-model.ts"]
async fn pinned_ner_public_boundary() -> Result<(), Box<dyn std::error::Error>> {
    let engine = Engine::from_detector(Detector::new()?.with_ner(
        "../../models/general-ner/model.onnx",
        "../../models/general-ner/tokenizer.json",
    )?);
    let text = "My name is John Smith and I work at Microsoft in London.";
    let policy = SecurityPolicy {
        mask_pii: true,
        block_secrets: true,
        contextual_pii: true,
        ..Default::default()
    };
    let result = engine
        .inspect_and_sanitize(Request::new(SanitizeRequest {
            request_id: "ner-fixture".into(),
            organization_id: "test".into(),
            text: text.into(),
            policy: Some(policy),
        }))
        .await?
        .into_inner();
    assert!(result.entity_types.iter().any(|kind| kind == "PERSON"));
    assert!(
        result
            .entity_types
            .iter()
            .any(|kind| kind == "ORGANIZATION")
    );
    assert!(!result.sanitized_text.contains("John Smith"));
    let restored = engine
        .de_anonymize(Request::new(DeAnonymizeRequest {
            request_id: "ner-fixture".into(),
            organization_id: "test".into(),
            text: result.sanitized_text,
            mappings: result.mappings,
        }))
        .await?
        .into_inner();
    assert_eq!(restored.text, text);
    let long = engine
        .inspect_and_sanitize(Request::new(SanitizeRequest {
            request_id: "long-fixture".into(),
            organization_id: "test".into(),
            text: "ordinary word ".repeat(600),
            policy: Some(SecurityPolicy {
                mask_pii: true,
                block_secrets: true,
                contextual_pii: true,
                ..Default::default()
            }),
        }))
        .await;
    assert_eq!(
        long.err().map(|error| error.code()),
        Some(tonic::Code::InvalidArgument)
    );
    Ok(())
}

#[tokio::test]
#[ignore = "requires pinned model: node --experimental-strip-types scripts/fetch-model.ts"]
async fn labeled_english_fixture_evaluation() -> Result<(), Box<dyn std::error::Error>> {
    let engine = Engine::from_detector(Detector::new()?.with_ner(
        "../../models/general-ner/model.onnx",
        "../../models/general-ner/tokenizer.json",
    )?);
    let fixtures: &[(&str, &[(&str, &str)])] = &[
        (
            "John Smith works at Microsoft in London.",
            &[
                ("John Smith", "PERSON"),
                ("Microsoft", "ORGANIZATION"),
                ("London", "LOCATION"),
            ],
        ),
        (
            "Barack Obama visited Paris.",
            &[("Barack Obama", "PERSON"), ("Paris", "LOCATION")],
        ),
        ("The meeting starts tomorrow.", &[]),
    ];
    let (mut correct, mut predicted, mut expected) = (0, 0, 0);
    for (index, (text, labels)) in fixtures.iter().enumerate() {
        let result = engine
            .inspect_and_sanitize(Request::new(SanitizeRequest {
                request_id: format!("evaluation-{index}"),
                organization_id: "fixture".into(),
                text: (*text).into(),
                policy: Some(SecurityPolicy {
                    mask_pii: true,
                    block_secrets: true,
                    contextual_pii: true,
                    ..Default::default()
                }),
            }))
            .await?
            .into_inner();
        expected += labels.len();
        predicted += result.mappings.len();
        for mapping in &result.mappings {
            assert_eq!(
                text.get(mapping.start_byte as usize..mapping.end_byte as usize),
                Some(mapping.original.as_str())
            );
            if labels
                .iter()
                .any(|(value, kind)| *value == mapping.original && *kind == mapping.entity_type)
            {
                correct += 1;
            }
        }
    }
    let precision = correct as f64 / predicted.max(1) as f64;
    let recall = correct as f64 / expected.max(1) as f64;
    println!(
        "Synthetic English fixtures: expected={expected}, predicted={predicted}, correct={correct}, precision={precision:.3}, recall={recall:.3}"
    );
    assert!(
        precision >= 0.8 && recall >= 0.8,
        "small fixture regression threshold"
    );
    Ok(())
}

#[tokio::test]
#[ignore = "requires pinned model: node --experimental-strip-types scripts/fetch-model.ts"]
async fn unicode_repetition_and_exact_token_boundary() -> Result<(), Box<dyn std::error::Error>> {
    let engine = Engine::from_detector(Detector::new()?.with_ner(
        "../../models/general-ner/model.onnx",
        "../../models/general-ner/tokenizer.json",
    )?);
    let tokenizer = tokenizers::Tokenizer::from_file("../../models/general-ner/tokenizer.json")
        .map_err(|_| "tokenizer load failed")?;
    let boundary = vec!["word"; 510].join(" ");
    assert_eq!(
        tokenizer
            .encode(boundary.as_str(), true)
            .map_err(|_| "encode failed")?
            .len(),
        512
    );
    for (index, text) in [
        "Éva met John Smith; John Smith visited Paris. 🛡️",
        "The meeting starts tomorrow.",
        boundary.as_str(),
    ]
    .into_iter()
    .enumerate()
    {
        let id = format!("roundtrip-{index}");
        let result = engine
            .inspect_and_sanitize(tonic::Request::new(SanitizeRequest {
                request_id: id.clone(),
                organization_id: "fixture".into(),
                text: text.into(),
                policy: Some(SecurityPolicy {
                    mask_pii: true,
                    block_secrets: true,
                    contextual_pii: true,
                    ..Default::default()
                }),
            }))
            .await?
            .into_inner();
        for mapping in &result.mappings {
            assert_eq!(
                text.get(mapping.start_byte as usize..mapping.end_byte as usize),
                Some(mapping.original.as_str())
            );
        }
        let restored = engine
            .de_anonymize(tonic::Request::new(DeAnonymizeRequest {
                request_id: id,
                organization_id: "fixture".into(),
                text: result.sanitized_text,
                mappings: result.mappings,
            }))
            .await?
            .into_inner();
        assert_eq!(restored.text, text);
    }
    Ok(())
}

#[tokio::test]
#[ignore = "requires pinned model: node --experimental-strip-types scripts/fetch-model.ts"]
async fn wordpiece_entities_are_fully_masked_and_restored() -> Result<(), Box<dyn std::error::Error>>
{
    let engine = Engine::from_detector(Detector::new()?.with_ner(
        "../../models/general-ner/model.onnx",
        "../../models/general-ner/tokenizer.json",
    )?);
    let tokenizer = tokenizers::Tokenizer::from_file("../../models/general-ner/tokenizer.json")
        .map_err(|_| "tokenizer load failed")?;
    let mut covered_split_words = 0;
    for (index, text) in [
        "Alexanderson met John Smith in London.",
        "Élodie Nakamura works at Microsoft in Seattle.",
        "Barack Obama met Schwarzenegger in California.",
        "John Smith visited Johannesburg; John Smith returned home.",
    ]
    .into_iter()
    .enumerate()
    {
        let encoded = tokenizer.encode(text, true).map_err(|_| "encode failed")?;
        let id = format!("whole-word-{index}");
        let response = engine
            .inspect_and_sanitize(Request::new(SanitizeRequest {
                request_id: id.clone(),
                organization_id: "fixture".into(),
                text: text.into(),
                policy: Some(SecurityPolicy {
                    mask_pii: true,
                    block_secrets: true,
                    contextual_pii: true,
                    ..Default::default()
                }),
            }))
            .await?
            .into_inner();
        let mut token = 0;
        while token < encoded.len() {
            let first = token;
            token += 1;
            while token < encoded.len()
                && encoded.get_word_ids()[first].is_some()
                && encoded.get_word_ids()[token] == encoded.get_word_ids()[first]
            {
                token += 1;
            }
            let start = encoded.get_offsets()[first].0;
            let end = encoded.get_offsets()[token - 1].1;
            for mapping in &response.mappings {
                let a = mapping.start_byte as usize;
                let b = mapping.end_byte as usize;
                if start < b && a < end {
                    assert!(
                        a <= start && end <= b,
                        "part of a tokenizer word remains unmasked"
                    );
                    covered_split_words += usize::from(token - first > 1);
                }
            }
        }
        let restored = engine
            .de_anonymize(Request::new(DeAnonymizeRequest {
                request_id: id,
                organization_id: "fixture".into(),
                text: response.sanitized_text,
                mappings: response.mappings,
            }))
            .await?
            .into_inner();
        assert_eq!(restored.text, text);
    }
    assert!(
        covered_split_words > 0,
        "fixture must exercise detected multi-piece words"
    );
    Ok(())
}
