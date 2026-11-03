use prompt_shield_engine::{
    service::Engine,
    wire::{security_engine_server::SecurityEngine, *},
};
use tonic::Request;

fn input(text: &str) -> SanitizeRequest {
    SanitizeRequest {
        request_id: "request-1".into(),
        organization_id: "org-1".into(),
        text: text.into(),
        policy: Some(SecurityPolicy {
            mask_pii: true,
            block_secrets: true,
            ..Default::default()
        }),
    }
}

#[tokio::test]
async fn masking_unicode_repetition_and_restoration() -> Result<(), Box<dyn std::error::Error>> {
    let engine = Engine::new()?;
    let text = "Merhaba 🌍 alice@example.com, alice@example.com / 4111 1111 1111 1111";
    let result = engine
        .inspect_and_sanitize(Request::new(input(text)))
        .await?
        .into_inner();
    assert_eq!(result.entity_count, 3);
    assert_eq!(result.mappings.len(), 2);
    assert!(!result.sanitized_text.contains("alice@example.com"));
    let restored = engine
        .de_anonymize(Request::new(DeAnonymizeRequest {
            request_id: "request-1".into(),
            organization_id: "org-1".into(),
            text: result.sanitized_text,
            mappings: result.mappings,
        }))
        .await?
        .into_inner();
    assert_eq!(restored.text, text);
    Ok(())
}

#[tokio::test]
async fn blocking_and_validation_are_fail_closed() -> Result<(), Box<dyn std::error::Error>> {
    let engine = Engine::new()?;
    let result = engine
        .inspect_and_sanitize(Request::new(input("sk-abcdefghijklmnopqrstuvwx")))
        .await?
        .into_inner();
    assert!(result.is_blocked);
    assert!(result.sanitized_text.is_empty());
    assert!(result.mappings.is_empty());
    let mut request = input("ordinary text");
    request.policy.as_mut().ok_or("policy")?.contextual_pii = true;
    assert_eq!(
        engine
            .inspect_and_sanitize(Request::new(request))
            .await
            .err()
            .ok_or("error")?
            .code(),
        tonic::Code::FailedPrecondition
    );
    let result = engine
        .inspect_and_sanitize(Request::new(input("4111 1111 1111 1112")))
        .await?
        .into_inner();
    assert_eq!(result.entity_count, 0);
    Ok(())
}

#[tokio::test]
async fn placeholders_are_isolated_between_requests() -> Result<(), Box<dyn std::error::Error>> {
    let engine = Engine::new()?;
    let a = engine
        .inspect_and_sanitize(Request::new(input("a@example.com")))
        .await?
        .into_inner();
    let b = engine
        .inspect_and_sanitize(Request::new(input("b@example.com")))
        .await?
        .into_inner();
    assert_ne!(a.mappings[0].placeholder, b.mappings[0].placeholder);
    let restored = engine
        .de_anonymize(Request::new(DeAnonymizeRequest {
            request_id: "request-1".into(),
            organization_id: "org-1".into(),
            text: a.sanitized_text.clone(),
            mappings: b.mappings,
        }))
        .await?
        .into_inner();
    assert_eq!(restored.text, a.sanitized_text);
    let scoped = engine
        .inspect_and_sanitize(Request::new(input("alice@example.com")))
        .await?
        .into_inner();
    for (request_id, organization_id) in [("other-request", "org-1"), ("request-1", "other-org")] {
        let result = engine
            .de_anonymize(Request::new(DeAnonymizeRequest {
                request_id: request_id.into(),
                organization_id: organization_id.into(),
                text: scoped.sanitized_text.clone(),
                mappings: scoped.mappings.clone(),
            }))
            .await;
        assert_eq!(
            result.err().ok_or("scope should fail")?.code(),
            tonic::Code::PermissionDenied
        );
    }
    Ok(())
}
