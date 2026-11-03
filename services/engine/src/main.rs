use prompt_shield_engine::{service::Engine, wire::security_engine_server::SecurityEngineServer};
use tonic::{Request, Status, transport::Server};

#[tokio::main]
async fn main() -> anyhow::Result<()> {
    let address = std::env::var("ENGINE_LISTEN")
        .unwrap_or_else(|_| "127.0.0.1:50051".into())
        .parse()?;
    let token = std::env::var("ENGINE_TOKEN")?;
    anyhow::ensure!(
        token.len() >= 32 && token.is_ascii(),
        "ENGINE_TOKEN must be at least 32 ASCII characters"
    );
    let detector = prompt_shield_engine::detector::Detector::new()?;
    #[cfg(feature = "ner")]
    let detector = if std::env::var("NER_ENABLED").is_ok_and(|value| value == "true") {
        detector.with_ner_config(
            &std::env::var("ONNX_MODEL_PATH")?,
            &std::env::var("TOKENIZER_CONFIG_PATH")?,
            prompt_shield_engine::ner::NerConfig::from_env()?,
        )?
    } else {
        detector
    };
    #[cfg(not(feature = "ner"))]
    anyhow::ensure!(
        !std::env::var("NER_ENABLED").is_ok_and(|value| value == "true"),
        "build with --features ner to enable contextual detection"
    );
    let engine = Engine::from_detector(detector);
    let contextual = engine.contextual_ready();
    let (reporter, health) = tonic_health::server::health_reporter();
    reporter.set_serving::<SecurityEngineServer<Engine>>().await;
    reporter
        .set_service_status(
            "promptshield.v1.Ner",
            if contextual {
                tonic_health::ServingStatus::Serving
            } else {
                tonic_health::ServingStatus::NotServing
            },
        )
        .await;
    let auth = move |request: Request<()>| -> Result<Request<()>, Status> {
        let supplied = request
            .metadata()
            .get("x-engine-token")
            .and_then(|v| v.to_str().ok())
            .unwrap_or_default();
        let difference = supplied
            .bytes()
            .zip(token.bytes())
            .fold(0u8, |diff, (a, b)| diff | (a ^ b));
        if supplied.len() != token.len() || difference != 0 {
            return Err(Status::unauthenticated("unauthorized"));
        }
        Ok(request)
    };
    println!("Prompt Shield engine listening on {address}; contextual_ready={contextual}");
    Server::builder()
        .concurrency_limit_per_connection(64)
        .add_service(health)
        .add_service(SecurityEngineServer::with_interceptor(engine, auth))
        .serve_with_shutdown(address, async {
            let _ = tokio::signal::ctrl_c().await;
        })
        .await?;
    Ok(())
}
