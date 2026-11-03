use crate::wire::Mapping;
use hmac::{Hmac, Mac};
use sha2::Sha256;
use tonic::Status;

pub fn key() -> [u8; 32] {
    let mut key = [0u8; 32];
    key[..16].copy_from_slice(uuid::Uuid::new_v4().as_bytes());
    key[16..].copy_from_slice(uuid::Uuid::new_v4().as_bytes());
    key
}

fn mac(
    key: &[u8],
    request: &str,
    organization: &str,
    mapping: &Mapping,
) -> Result<Hmac<Sha256>, Status> {
    let mut mac = Hmac::<Sha256>::new_from_slice(key)
        .map_err(|_| Status::internal("mapping_authentication_failed"))?;
    for part in [
        request,
        organization,
        &mapping.placeholder,
        &mapping.original,
        &mapping.entity_type,
    ] {
        mac.update(&(part.len() as u64).to_be_bytes());
        mac.update(part.as_bytes());
    }
    mac.update(&mapping.start_byte.to_be_bytes());
    mac.update(&mapping.end_byte.to_be_bytes());
    Ok(mac)
}
pub fn sign(
    key: &[u8],
    request: &str,
    organization: &str,
    mappings: &mut [Mapping],
) -> Result<(), Status> {
    for mapping in mappings {
        mapping.proof = mac(key, request, organization, mapping)?
            .finalize()
            .into_bytes()
            .to_vec();
    }
    Ok(())
}
pub fn verify(
    key: &[u8],
    request: &str,
    organization: &str,
    mappings: &[Mapping],
) -> Result<(), Status> {
    for mapping in mappings {
        mac(key, request, organization, mapping)?
            .verify_slice(&mapping.proof)
            .map_err(|_| Status::permission_denied("mapping_scope_invalid"))?;
    }
    Ok(())
}
