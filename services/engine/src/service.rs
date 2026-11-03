use crate::{
    detector::{Detector, InspectError, Inspection, Prepared, restore},
    wire::{security_engine_server::SecurityEngine, *},
};
use std::{sync::Arc, time::Instant};
use tokio::sync::Semaphore;
use tonic::{Request, Response, Status};

pub struct Engine {
    signing_key: [u8; 32],
    detector: Arc<Detector>,
    permits: Arc<Semaphore>,
    #[cfg(feature = "ner")]
    ner_permits: Arc<Semaphore>,
}
impl Engine {
    pub fn new() -> Result<Self, InspectError> {
        Ok(Self::from_detector(Detector::new()?))
    }
    pub fn from_detector(detector: Detector) -> Self {
        Self {
            signing_key: crate::mapping::key(),
            detector: Arc::new(detector),
            permits: Arc::new(Semaphore::new(4)),
            #[cfg(feature = "ner")]
            ner_permits: Arc::new(Semaphore::new(4)),
        }
    }
    pub fn contextual_ready(&self) -> bool {
        self.detector.contextual_ready()
    }

    /// Read-only observation for offline cancellation profiling.
    #[cfg(feature = "evaluation")]
    pub fn evaluation_inflight(&self) -> usize {
        8 - self.permits.available_permits() - self.ner_permits.available_permits()
    }

    #[cfg(feature = "evaluation")]
    pub fn evaluation_ner_inflight(&self) -> usize {
        4 - self.ner_permits.available_permits()
    }
}

#[cfg(feature = "ner")]
struct CancelRun(Arc<ort::session::run_options::RunOptions>);

#[cfg(feature = "ner")]
impl Drop for CancelRun {
    fn drop(&mut self) {
        // Request-specific options are never reused. Keep the worker lease in
        // spawn_blocking until native computation has actually stopped.
        let _ = self.0.terminate();
    }
}

fn response(
    mut result: Inspection<'_>,
    input: &SanitizeRequest,
    signing_key: &[u8; 32],
    started: Instant,
) -> Result<SanitizeResponse, Status> {
    let stage = crate::profile::Stage::new("signing");
    crate::mapping::sign(
        signing_key,
        &input.request_id,
        &input.organization_id,
        &mut result.mappings,
    )?;
    drop(stage);
    Ok(SanitizeResponse {
        sanitized_text: result.text.into_owned(),
        is_blocked: result.blocked,
        reason_code: result.reason.into(),
        mappings: result.mappings,
        entity_types: result.types.into_iter().map(String::from).collect(),
        entity_count: result.count as u32,
        processing_time_micros: started.elapsed().as_micros() as u64,
    })
}

fn status(error: InspectError) -> Status {
    match error {
        InspectError::ModelUnavailable => {
            Status::failed_precondition("contextual_model_unavailable")
        }
        InspectError::InvalidInput | InspectError::InvalidPolicy => {
            Status::invalid_argument("invalid_request")
        }
        InspectError::Initialization => Status::internal("inspection_failed"),
    }
}
fn identity(request: &str, organization: &str) -> Result<(), Status> {
    if request.is_empty()
        || request.len() > 100
        || organization.is_empty()
        || organization.len() > 100
    {
        return Err(Status::invalid_argument("invalid_identity"));
    }
    Ok(())
}

#[tonic::async_trait]
impl SecurityEngine for Engine {
    async fn inspect_and_sanitize(
        &self,
        request: Request<SanitizeRequest>,
    ) -> Result<Response<SanitizeResponse>, Status> {
        let started = Instant::now();
        let mut input = request.into_inner();
        identity(&input.request_id, &input.organization_id)?;
        let policy = input
            .policy
            .take()
            .ok_or_else(|| Status::invalid_argument("policy_required"))?;
        let permit = Arc::clone(&self.permits)
            .try_acquire_owned()
            .map_err(|_| Status::resource_exhausted("engine_busy"))?;
        let detector = Arc::clone(&self.detector);
        let signing_key = self.signing_key;
        let prepared = tokio::task::spawn_blocking(move || {
            let _permit = permit;
            let prepared = detector.prepare(&input.text, &policy).map_err(status)?;
            match prepared {
                Prepared::Blocked(result) => {
                    Ok(Ok(response(result, &input, &signing_key, started)?))
                }
                Prepared::Ready(spans) if !policy.contextual_pii => {
                    let result = Detector::finish(&input.text, spans).map_err(status)?;
                    Ok(Ok(response(result, &input, &signing_key, started)?))
                }
                Prepared::Ready(spans) => Ok::<_, Status>(Err((input, spans))),
            }
        })
        .await
        .map_err(|_| Status::internal("inspection_failed"))??;
        match prepared {
            Ok(result) => Ok(Response::new(result)),
            Err((input, mut spans)) => {
                #[cfg(feature = "ner")]
                {
                    let capacity = Arc::clone(&self.ner_permits)
                        .try_acquire_owned()
                        .map_err(|_| Status::resource_exhausted("ner_busy"))?;
                    let ner = self.detector.ner().map_err(status)?;
                    let mut lease = ner.acquire().await.map_err(status)?;
                    let options = Arc::new(
                        ort::session::run_options::RunOptions::new()
                            .map_err(|_| Status::internal("inspection_failed"))?,
                    );
                    let cancel = CancelRun(Arc::clone(&options));
                    let detector = Arc::clone(&self.detector);
                    let result = tokio::task::spawn_blocking(move || {
                        let _capacity = capacity;
                        spans.extend(
                            detector
                                .ner()
                                .map_err(status)?
                                .detect_with(&input.text, &mut lease, &options)
                                .map_err(status)?,
                        );
                        let result = Detector::finish(&input.text, spans).map_err(status)?;
                        response(result, &input, &signing_key, started)
                    })
                    .await
                    .map_err(|_| Status::internal("inspection_failed"))??;
                    drop(cancel);
                    Ok(Response::new(result))
                }
                #[cfg(not(feature = "ner"))]
                {
                    let _ = (&input, &mut spans);
                    Err(status(InspectError::ModelUnavailable))
                }
            }
        }
    }
    async fn de_anonymize(
        &self,
        request: Request<DeAnonymizeRequest>,
    ) -> Result<Response<DeAnonymizeResponse>, Status> {
        let input = request.into_inner();
        identity(&input.request_id, &input.organization_id)?;
        if input.mappings.len() > 16384 {
            return Err(Status::invalid_argument("invalid_mapping_count"));
        }
        crate::mapping::verify(
            &self.signing_key,
            &input.request_id,
            &input.organization_id,
            &input.mappings,
        )?;
        Ok(Response::new(DeAnonymizeResponse {
            text: restore(&input.text, &input.mappings).map_err(status)?,
        }))
    }
}
