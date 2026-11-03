//! Offline evaluation: input text is never included in the output report.
use anyhow::{Context, Result, bail};
use prompt_shield_engine::{detector::InspectError, ner::Ner};
use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::{
    collections::BTreeMap,
    fs::File,
    io::{BufRead, BufReader},
    path::PathBuf,
    time::Instant,
};

#[derive(Deserialize)]
struct Artifact {
    model: String,
    tokenizer: String,
    model_sha256: String,
    tokenizer_sha256: String,
    #[serde(default)]
    config: Option<String>,
    #[serde(default)]
    config_sha256: Option<String>,
    #[serde(default)]
    inputs: Vec<String>,
}
#[derive(Deserialize)]
struct Example {
    id: String,
    text: String,
    entities: Vec<(usize, usize, String)>,
}
#[derive(Default, Serialize)]
struct Counts {
    gold: usize,
    predicted: usize,
    matched: usize,
}
#[derive(Serialize)]
struct Prediction {
    id: String,
    spans: Vec<(usize, usize, String)>,
}

fn main() -> Result<()> {
    let args: Vec<_> = std::env::args_os().skip(1).collect();
    if args.first().is_some_and(|s| s == "workloads") && args.len() == 2 {
        return workloads(PathBuf::from(&args[1]));
    }
    if args.len() != 2 {
        bail!("usage: ner-evaluate ARTIFACT.json CORPUS.jsonl");
    }
    let manifest_path = PathBuf::from(&args[0]);
    let manifest: Artifact = serde_json::from_reader(File::open(&manifest_path)?)?;
    let base = manifest_path.parent().context("manifest parent")?;
    let model = base.join(&manifest.model);
    let tokenizer = base.join(&manifest.tokenizer);
    let ner = Ner::load_for_evaluation(
        model.to_str().context("model path")?,
        tokenizer.to_str().context("tokenizer path")?,
        &manifest.model_sha256,
        &manifest.tokenizer_sha256,
    )?;
    let ner = if let Some(config) = &manifest.config {
        let bytes = std::fs::read(base.join(config))?;
        let digest = format!("{:x}", Sha256::digest(&bytes));
        anyhow::ensure!(
            Some(&digest) == manifest.config_sha256.as_ref(),
            "config digest mismatch"
        );
        let config: serde_json::Value = serde_json::from_slice(&bytes)?;
        let labels: Vec<String> = (0..9)
            .map(|index| {
                config["id2label"][index.to_string()]
                    .as_str()
                    .map(String::from)
                    .context("missing label")
            })
            .collect::<Result<_>>()?;
        ner.with_evaluation_schema(&labels, &manifest.inputs)?
    } else {
        ner
    };
    let mut counts: BTreeMap<String, Counts> = ["PERSON", "ORGANIZATION", "LOCATION"]
        .into_iter()
        .map(|k| (k.into(), Counts::default()))
        .collect();
    let mut predictions = Vec::new();
    let mut rejected = Vec::new();
    let mut corpus_hash = Sha256::new();
    let started = Instant::now();
    for line in BufReader::new(File::open(&args[1])?).lines() {
        let line = line?;
        corpus_hash.update(line.as_bytes());
        corpus_hash.update(b"\n");
        let row: Example = serde_json::from_str(&line)?;
        for (start, end, kind) in &row.entities {
            if start >= end
                || !row.text.is_char_boundary(*start)
                || !row.text.is_char_boundary(*end)
                || !counts.contains_key(kind)
            {
                bail!("invalid annotated span for {}", row.id);
            }
        }
        let spans = match ner.detect(&row.text) {
            Ok(spans) => spans,
            Err(InspectError::InvalidInput) => {
                rejected.push(row.id);
                continue;
            }
            Err(error) => return Err(error.into()),
        };
        for kind in ["PERSON", "ORGANIZATION", "LOCATION"] {
            let count = counts.get_mut(kind).context("supported class")?;
            count.gold += row.entities.iter().filter(|s| s.2 == kind).count();
            count.predicted += spans.iter().filter(|s| s.2 == kind).count();
            count.matched += spans
                .iter()
                .filter(|s| {
                    s.2 == kind
                        && row
                            .entities
                            .iter()
                            .any(|g| g.0 == s.0 && g.1 == s.1 && g.2 == s.2)
                })
                .count();
        }
        predictions.push(Prediction {
            id: row.id,
            spans: spans
                .into_iter()
                .map(|(a, b, k)| (a, b, k.into()))
                .collect(),
        });
    }
    if predictions.is_empty() {
        bail!("no evaluable examples");
    }
    let prediction_hash = format!("{:x}", Sha256::digest(serde_json::to_vec(&predictions)?));
    let report = serde_json::json!({
        "schema_version": 1, "artifact_sha256": manifest.model_sha256,
        "tokenizer_sha256": manifest.tokenizer_sha256, "corpus_sha256": format!("{:x}", corpus_hash.finalize()),
        "evaluated": predictions.len(), "rejected_ids": rejected, "counts": counts,
        "prediction_sha256": prediction_hash, "predictions": predictions,
        "duration_seconds": started.elapsed().as_secs_f64(), "model_bytes": model.metadata()?.len(),
        "intra_threads": 4, "optimization": "Level3", "confidence_threshold": 0.5,
        "span_aggregation": prompt_shield_engine::ner::SPAN_AGGREGATION,
        "os": std::env::consts::OS, "architecture": std::env::consts::ARCH,
    });
    serde_json::to_writer(std::io::stdout().lock(), &report)?;
    Ok(())
}

fn workloads(path: PathBuf) -> Result<()> {
    let mut tokenizer =
        tokenizers::Tokenizer::from_file(path).map_err(|_| anyhow::anyhow!("tokenizer load"))?;
    tokenizer
        .with_truncation(None)
        .map_err(|_| anyhow::anyhow!("tokenizer truncation"))?;
    tokenizer.with_padding(None);
    let mut cases = Vec::new();
    for size in [64, 1024, 16384, 131072] {
        for (kind, prefix, expected) in [
            ("clean", "Explain a rainbow. ", "allowed"),
            ("masked", "Email alice@example.com. ", "masked"),
            ("blocked", "sk-abcdefghijklmnopqrstuvwx ", "blocked"),
        ] {
            let mut text = String::from(prefix);
            text.push_str(&" ".repeat(size - text.len()));
            cases.push(
                serde_json::json!({"name": format!("{kind}-{size}"), "text": text,
                "contextual": false, "expected": expected, "input_bytes": size}),
            );
        }
    }
    for target in [32, 128, 512, 513] {
        for sentence in [
            "John Smith works at Microsoft in London.",
            "Alice Johnson visited Paris with David Miller.",
            "Sarah Wilson met Robert Brown in Berlin.",
            "Michael Davis joined Google in New York.",
            "Emma Thompson traveled from London to Rome.",
            "James Anderson works for Amazon in Seattle.",
            "Maria Garcia met Peter Jones in Madrid.",
            "Daniel Taylor visited Microsoft in Boston.",
        ] {
            let mut text = String::from(sentence);
            for _ in 0..514 {
                let count = tokenizer
                    .encode(text.as_str(), true)
                    .map_err(|_| anyhow::anyhow!("tokenization"))?
                    .len();
                if count == target {
                    break;
                }
                if count > target {
                    bail!("cannot construct exact token length");
                }
                text.push_str(" word");
            }
            anyhow::ensure!(
                tokenizer
                    .encode(text.as_str(), true)
                    .map_err(|_| anyhow::anyhow!("tokenization"))?
                    .len()
                    == target,
                "cannot construct exact token length"
            );
            cases.push(
                serde_json::json!({"name": format!("ner-{target}"), "input_bytes": text.len(),
            "text": text, "contextual": true, "tokens": target,
            "expected": if target > 512 { "invalid" } else { "masked" }}),
            );
        }
    }
    cases.push(
        serde_json::json!({"name": "rules-over-limit", "text": " ".repeat(131073),
        "contextual": false, "expected": "invalid", "input_bytes": 131073}),
    );
    serde_json::to_writer(std::io::stdout().lock(), &cases)?;
    Ok(())
}
