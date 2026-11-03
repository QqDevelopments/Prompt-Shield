pub mod detector;
mod mapping;
#[cfg(feature = "ner")]
pub mod ner;
mod profile;
pub mod service;
pub mod wire {
    tonic::include_proto!("promptshield.v1");
}
