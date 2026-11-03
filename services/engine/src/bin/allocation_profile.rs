//! Separate profiling executable; its instrumentation is absent from production.
use anyhow::{Context, Result, bail};
use prompt_shield_engine::{
    detector::Detector,
    service::Engine,
    wire::{SanitizeRequest, SecurityPolicy, security_engine_server::SecurityEngine},
};
use serde::Deserialize;
use std::{
    alloc::{GlobalAlloc, Layout, System},
    sync::atomic::{AtomicU64, Ordering},
};

struct CountingAllocator;
static CALLS: AtomicU64 = AtomicU64::new(0);
static BYTES: AtomicU64 = AtomicU64::new(0);
// SAFETY: all allocations are delegated unchanged to the system allocator.
unsafe impl GlobalAlloc for CountingAllocator {
    unsafe fn alloc(&self, layout: Layout) -> *mut u8 {
        CALLS.fetch_add(1, Ordering::Relaxed);
        BYTES.fetch_add(layout.size() as u64, Ordering::Relaxed);
        unsafe { System.alloc(layout) }
    }
    unsafe fn alloc_zeroed(&self, layout: Layout) -> *mut u8 {
        CALLS.fetch_add(1, Ordering::Relaxed);
        BYTES.fetch_add(layout.size() as u64, Ordering::Relaxed);
        unsafe { System.alloc_zeroed(layout) }
    }
    unsafe fn realloc(&self, ptr: *mut u8, layout: Layout, size: usize) -> *mut u8 {
        CALLS.fetch_add(1, Ordering::Relaxed);
        BYTES.fetch_add(size as u64, Ordering::Relaxed);
        unsafe { System.realloc(ptr, layout, size) }
    }
    unsafe fn dealloc(&self, ptr: *mut u8, layout: Layout) {
        unsafe { System.dealloc(ptr, layout) }
    }
}
#[global_allocator]
static ALLOCATOR: CountingAllocator = CountingAllocator;
#[derive(Deserialize)]
struct Case {
    name: String,
    text: String,
    contextual: bool,
}

#[tokio::main]
async fn main() -> Result<()> {
    let args: Vec<String> = std::env::args().skip(1).collect();
    if args.len() != 3 {
        bail!("usage: allocation-profile CASES.json CASE ITERATIONS");
    }
    let cases: Vec<Case> = serde_json::from_reader(std::fs::File::open(&args[0])?)?;
    let case = cases
        .into_iter()
        .find(|c| c.name == args[1])
        .context("unknown case")?;
    let iterations: u64 = args[2].parse()?;
    if iterations == 0 {
        bail!("iterations must be positive");
    }
    let mut detector = Detector::new()?;
    if case.contextual {
        detector = detector.with_ner(
            &std::env::var("ONNX_MODEL_PATH")?,
            &std::env::var("TOKENIZER_CONFIG_PATH")?,
        )?;
    }
    let engine = Engine::from_detector(detector);
    let inspect = || {
        engine.inspect_and_sanitize(tonic::Request::new(SanitizeRequest {
            request_id: "allocation-fixture".into(),
            organization_id: "allocation-fixture".into(),
            text: case.text.clone(),
            policy: Some(SecurityPolicy {
                mask_pii: true,
                block_secrets: true,
                contextual_pii: case.contextual,
                ..Default::default()
            }),
        }))
    };
    for _ in 0..20 {
        drop(inspect().await?);
    }
    let calls = CALLS.load(Ordering::Relaxed);
    let bytes = BYTES.load(Ordering::Relaxed);
    for _ in 0..iterations {
        drop(inspect().await?);
    }
    let calls = CALLS.load(Ordering::Relaxed) - calls;
    let bytes = BYTES.load(Ordering::Relaxed) - bytes;
    println!(
        "{}",
        serde_json::json!({"schema_version":1,"case":case.name,"iterations":iterations,
        "allocations":calls,"allocated_bytes":bytes,"allocations_per_request":calls as f64/iterations as f64,
        "bytes_per_request":bytes as f64/iterations as f64,"includes_request_construction":true,
        "scope":"Rust global allocator across public service dispatch; excludes native ONNX allocations",
        "native_onnx_allocations_included":false})
    );
    Ok(())
}
