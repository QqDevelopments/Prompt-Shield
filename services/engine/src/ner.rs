use crate::detector::InspectError;
use ort::{
    session::{Session, builder::GraphOptimizationLevel, run_options::RunOptions},
    value::TensorRef,
};
use sha2::{Digest, Sha256};
use std::sync::{Arc, Mutex};
use std::{
    fs::File,
    io::{BufReader, Read},
};
use tokenizers::Tokenizer;
use tokio::sync::{OwnedSemaphorePermit, Semaphore};

fn verify(path: &str, expected: &str) -> Result<(), InspectError> {
    let file = File::open(path).map_err(|_| InspectError::Initialization)?;
    let mut reader = BufReader::new(file);
    let mut digest = Sha256::new();
    let mut buffer = [0u8; 65536];
    loop {
        let read = reader
            .read(&mut buffer)
            .map_err(|_| InspectError::Initialization)?;
        if read == 0 {
            break;
        }
        digest.update(&buffer[..read]);
    }
    if format!("{:x}", digest.finalize()) != expected {
        return Err(InspectError::Initialization);
    }
    Ok(())
}

pub type Span = (usize, usize, &'static str);
pub const SPAN_AGGREGATION: &str = "first_confident_entity_wordpiece_v1";
#[derive(Clone, Copy, Debug)]
pub struct NerConfig {
    pub sessions: usize,
    pub intra_threads: usize,
    pub spinning: bool,
}

impl Default for NerConfig {
    fn default() -> Self {
        Self {
            sessions: 1,
            intra_threads: 4,
            spinning: true,
        }
    }
}

impl NerConfig {
    pub fn validate(self) -> Result<Self, InspectError> {
        if ![1, 2, 4].contains(&self.sessions)
            || ![1, 2, 4].contains(&self.intra_threads)
            || self.sessions * self.intra_threads > 8
        {
            return Err(InspectError::Initialization);
        }
        Ok(self)
    }

    pub fn from_env() -> Result<Self, InspectError> {
        let mut config = Self::default();
        for (name, setting) in [
            ("NER_SESSION_COUNT", &mut config.sessions),
            ("NER_INTRA_THREADS", &mut config.intra_threads),
        ] {
            match std::env::var(name) {
                Ok(value) => *setting = value.parse().map_err(|_| InspectError::Initialization)?,
                Err(std::env::VarError::NotPresent) => (),
                Err(_) => return Err(InspectError::Initialization),
            }
        }
        match std::env::var("NER_SPINNING") {
            Ok(value) => {
                config.spinning = value.parse().map_err(|_| InspectError::Initialization)?
            }
            Err(std::env::VarError::NotPresent) => (),
            Err(_) => return Err(InspectError::Initialization),
        }
        config.validate()
    }
}

struct Worker {
    session: Session,
    ids: [i64; 512],
    mask: [i64; 512],
    types: [i64; 512],
}

struct Pool {
    workers: Mutex<Vec<Box<Worker>>>,
    available: Arc<Semaphore>,
}

pub(crate) struct Lease {
    worker: Option<Box<Worker>>,
    pool: Arc<Pool>,
    _permit: OwnedSemaphorePermit,
}

impl Drop for Lease {
    fn drop(&mut self) {
        if let Some(mut worker) = self.worker.take() {
            worker.ids.fill(0);
            worker.mask.fill(0);
            worker.types.fill(0);
            // Recover storage after a panic so capacity is not silently leaked.
            let mut workers = self
                .pool
                .workers
                .lock()
                .unwrap_or_else(|error| error.into_inner());
            workers.push(worker);
        }
    }
}

pub struct Ner {
    pool: Arc<Pool>,
    tokenizer: Arc<Tokenizer>,
    #[cfg(feature = "evaluation")]
    label_order: Option<[usize; 9]>,
    #[cfg(feature = "evaluation")]
    input_names: Vec<String>,
}

impl Ner {
    pub fn load(model: &str, tokenizer: &str) -> Result<Self, InspectError> {
        Self::load_config(model, tokenizer, NerConfig::default())
    }

    pub fn load_config(
        model: &str,
        tokenizer: &str,
        config: NerConfig,
    ) -> Result<Self, InspectError> {
        Self::load_verified(
            model,
            tokenizer,
            "caaee70a5518ec7f9e46e5308fcc9263a8c227703a9ce46cf61c69a552349648",
            "343989712a36cd8b253efeaf8baf6a08b9d2583f78e395e83832e8ee9f8d8ee1",
            config,
        )
    }

    /// Offline evaluation only; production still accepts only its pinned artifact.
    #[cfg(feature = "evaluation")]
    pub fn load_for_evaluation(
        model: &str,
        tokenizer: &str,
        model_sha256: &str,
        tokenizer_sha256: &str,
    ) -> Result<Self, InspectError> {
        Self::load_verified(
            model,
            tokenizer,
            model_sha256,
            tokenizer_sha256,
            NerConfig::default(),
        )
    }

    /// Offline-only schema validation. Production artifacts remain hash-pinned.
    #[cfg(feature = "evaluation")]
    pub fn with_evaluation_schema(
        mut self,
        labels: &[String],
        inputs: &[String],
    ) -> Result<Self, InspectError> {
        const CANONICAL: [&str; 9] = [
            "O", "B-MISC", "I-MISC", "B-PER", "I-PER", "B-ORG", "I-ORG", "B-LOC", "I-LOC",
        ];
        if labels.len() != 9 || inputs != self.input_names {
            return Err(InspectError::Initialization);
        }
        let mut order = [0; 9];
        for (index, label) in CANONICAL.iter().enumerate() {
            order[index] = labels
                .iter()
                .position(|value| value == label)
                .ok_or(InspectError::Initialization)?;
        }
        self.label_order = Some(order);
        Ok(self)
    }

    fn load_verified(
        model: &str,
        tokenizer: &str,
        model_sha256: &str,
        tokenizer_sha256: &str,
        config: NerConfig,
    ) -> Result<Self, InspectError> {
        let config = config.validate()?;
        verify(model, model_sha256)?;
        verify(tokenizer, tokenizer_sha256)?;
        let mut workers = Vec::with_capacity(config.sessions);
        for _ in 0..config.sessions {
            let session = Session::builder()
                .map_err(|_| InspectError::Initialization)?
                .with_optimization_level(GraphOptimizationLevel::Level3)
                .map_err(|_| InspectError::Initialization)?
                .with_intra_threads(config.intra_threads)
                .map_err(|_| InspectError::Initialization)?
                .with_intra_op_spinning(config.spinning)
                .map_err(|_| InspectError::Initialization)?
                .with_inter_op_spinning(config.spinning)
                .map_err(|_| InspectError::Initialization)?
                .commit_from_file(model)
                .map_err(|_| InspectError::Initialization)?;
            workers.push(Box::new(Worker {
                session,
                ids: [0; 512],
                mask: [0; 512],
                types: [0; 512],
            }));
        }
        let mut tokenizer =
            Tokenizer::from_file(tokenizer).map_err(|_| InspectError::Initialization)?;
        tokenizer
            .with_truncation(None)
            .map_err(|_| InspectError::Initialization)?;
        tokenizer.with_padding(None);
        #[cfg(feature = "evaluation")]
        let input_names = workers
            .first()
            .ok_or(InspectError::Initialization)?
            .session
            .inputs
            .iter()
            .map(|input| input.name.clone())
            .collect();
        Ok(Self {
            pool: Arc::new(Pool {
                workers: Mutex::new(workers),
                available: Arc::new(Semaphore::new(config.sessions)),
            }),
            tokenizer: Arc::new(tokenizer),
            #[cfg(feature = "evaluation")]
            label_order: None,
            #[cfg(feature = "evaluation")]
            input_names,
        })
    }

    pub fn detect(&self, text: &str) -> Result<Vec<Span>, InspectError> {
        // Synchronous offline callers do not enqueue native work. The public
        // service uses acquire(), which waits asynchronously before dispatch.
        let permit = Arc::clone(&self.pool.available)
            .try_acquire_owned()
            .map_err(|_| InspectError::ModelUnavailable)?;
        let mut lease = self.lease(permit)?;
        let options = RunOptions::new().map_err(|_| InspectError::Initialization)?;
        self.detect_with(text, &mut lease, &options)
    }

    fn lease(&self, permit: OwnedSemaphorePermit) -> Result<Lease, InspectError> {
        let worker = self
            .pool
            .workers
            .lock()
            .map_err(|_| InspectError::ModelUnavailable)?
            .pop()
            .ok_or(InspectError::ModelUnavailable)?;
        Ok(Lease {
            worker: Some(worker),
            pool: Arc::clone(&self.pool),
            _permit: permit,
        })
    }

    pub(crate) async fn acquire(&self) -> Result<Lease, InspectError> {
        let _stage = crate::profile::Stage::new("session_wait");
        let permit = Arc::clone(&self.pool.available)
            .acquire_owned()
            .await
            .map_err(|_| InspectError::ModelUnavailable)?;
        self.lease(permit)
    }

    pub(crate) fn detect_with(
        &self,
        text: &str,
        lease: &mut Lease,
        options: &RunOptions,
    ) -> Result<Vec<Span>, InspectError> {
        let stage = crate::profile::Stage::new("tokenization");
        let encoded = self
            .tokenizer
            .encode(text, true)
            .map_err(|_| InspectError::InvalidInput)?;
        let length = encoded.len();
        if length > 512 {
            return Err(InspectError::InvalidInput);
        }
        drop(stage);
        let stage = crate::profile::Stage::new("tensor_prepare");
        let worker = lease
            .worker
            .as_mut()
            .ok_or(InspectError::ModelUnavailable)?;
        for index in 0..length {
            worker.ids[index] = i64::from(encoded.get_ids()[index]);
            worker.mask[index] = i64::from(encoded.get_attention_mask()[index]);
            worker.types[index] = i64::from(encoded.get_type_ids()[index]);
        }
        let ids = TensorRef::from_array_view(([1, length], &worker.ids[..length]))
            .map_err(|_| InspectError::Initialization)?;
        let mask = TensorRef::from_array_view(([1, length], &worker.mask[..length]))
            .map_err(|_| InspectError::Initialization)?;
        let types = TensorRef::from_array_view(([1, length], &worker.types[..length]))
            .map_err(|_| InspectError::Initialization)?;
        drop(stage);
        let stage = crate::profile::Stage::new("onnx");
        let inputs =
            ort::inputs!["input_ids" => ids, "attention_mask" => mask, "token_type_ids" => types];
        #[cfg(feature = "evaluation")]
        let inputs = {
            let mut inputs = inputs;
            if self.label_order.is_some() && self.input_names == ["input_ids", "attention_mask"] {
                inputs.pop();
            }
            inputs
        };
        let outputs = worker
            .session
            .run_with_options(inputs, options)
            .map_err(|_| InspectError::ModelUnavailable)?;
        drop(stage);
        let _stage = crate::profile::Stage::new("span_decode");
        let tensor = outputs.get("logits").ok_or(InspectError::Initialization)?;
        let (shape, logits) = tensor
            .try_extract_tensor::<f32>()
            .map_err(|_| InspectError::Initialization)?;
        if shape.len() != 3 || shape[0] != 1 || shape[1] != length as i64 || shape[2] != 9 {
            return Err(InspectError::Initialization);
        }
        #[cfg(feature = "evaluation")]
        if let Some(order) = self.label_order {
            // Candidate-only canonical projection uses the same production
            // wordpiece decoder, confidence threshold and masking semantics.
            let canonical: Vec<f32> = logits
                .as_chunks::<9>()
                .0
                .iter()
                .flat_map(|row| order.map(|index| row[index]))
                .collect();
            return decode_words(
                text,
                encoded.get_offsets(),
                encoded.get_word_ids(),
                &canonical,
            );
        }
        decode_words(text, encoded.get_offsets(), encoded.get_word_ids(), logits)
    }
}

// A confident entity subtoken protects its entire tokenizer word. Quantization
// can change the label of a suffix; emitting partial words would expose it.
fn decode_words(
    text: &str,
    offsets: &[(usize, usize)],
    words: &[Option<u32>],
    logits: &[f32],
) -> Result<Vec<Span>, InspectError> {
    if offsets.len() != words.len() || logits.len() != offsets.len() * 9 {
        return Err(InspectError::Initialization);
    }
    let mut spans: Vec<Span> = Vec::new();
    let mut index = 0;
    while index < offsets.len() {
        let first = index;
        index += 1;
        while index < offsets.len() && words[first].is_some() && words[index] == words[first] {
            index += 1;
        }
        let mut word_label = None;
        for token in first..index {
            let (start, end) = offsets[token];
            if start == end {
                continue;
            }
            if start > end
                || !text.is_char_boundary(start)
                || !text.is_char_boundary(end)
                || end > text.len()
            {
                return Err(InspectError::InvalidInput);
            }
            let scores = &logits[token * 9..(token + 1) * 9];
            if scores.iter().any(|score| !score.is_finite()) {
                return Err(InspectError::ModelUnavailable);
            }
            let label = scores
                .iter()
                .enumerate()
                .max_by(|a, b| a.1.total_cmp(b.1))
                .map(|(label, _)| label)
                .ok_or(InspectError::Initialization)?;
            // Pinned dslim/bert-base-NER labels: O, B/I-MISC, B/I-PER, B/I-ORG, B/I-LOC.
            if !(3..=8).contains(&label) {
                continue;
            }
            let confidence = 1.0
                / scores
                    .iter()
                    .map(|score| (*score - scores[label]).exp())
                    .sum::<f32>();
            if confidence < 0.5 {
                continue;
            }
            // Preserve the BIO decision of the first confident entity piece.
            word_label.get_or_insert(label);
        }
        let Some(label) = word_label else { continue };
        let kind = match label {
            3 | 4 => "PERSON",
            5 | 6 => "ORGANIZATION",
            7 | 8 => "LOCATION",
            _ => return Err(InspectError::Initialization),
        };
        let start = offsets[first].0;
        let end = offsets[index - 1].1;
        if label.is_multiple_of(2)
            && let Some(previous) = spans.last_mut()
            && previous.2 == kind
            && previous.1 <= start
            && text[previous.1..start].chars().all(char::is_whitespace)
        {
            previous.1 = end;
            continue;
        }
        spans.push((start, end, kind));
    }
    Ok(spans)
}

#[cfg(test)]
mod tests {
    use super::*;

    fn scores(labels: &[usize]) -> Vec<f32> {
        let mut values = vec![0.0; labels.len() * 9];
        for (index, label) in labels.iter().enumerate() {
            values[index * 9 + label] = 10.0;
        }
        values
    }

    #[test]
    fn rejected_suffix_and_prefix_do_not_expose_part_of_a_word() -> Result<(), InspectError> {
        for labels in [[3, 0], [0, 4], [3, 5]] {
            let spans = decode_words(
                "Élodie",
                &[(0, 2), (2, 7)],
                &[Some(0), Some(0)],
                &scores(&labels),
            )?;
            assert_eq!(spans, vec![(0, 7, "PERSON")]);
        }
        Ok(())
    }

    #[test]
    fn adjacent_words_and_bio_boundaries_remain_distinct() -> Result<(), InspectError> {
        let offsets = [(0, 3), (4, 7), (8, 13)];
        let words = [Some(0), Some(1), Some(2)];
        assert_eq!(
            decode_words("Ada Bob waits", &offsets, &words, &scores(&[3, 3, 0]))?,
            vec![(0, 3, "PERSON"), (4, 7, "PERSON")]
        );
        assert_eq!(
            decode_words("Ada Bob waits", &offsets, &words, &scores(&[3, 4, 0]))?,
            vec![(0, 7, "PERSON")]
        );
        Ok(())
    }

    #[test]
    fn uncertain_words_are_not_promoted_and_invalid_scores_fail_closed() -> Result<(), InspectError>
    {
        assert!(decode_words("Ada", &[(0, 3)], &[Some(0)], &[0.0; 9])?.is_empty());
        let mut logits = scores(&[3, 0]);
        logits[9] = f32::NAN;
        assert!(matches!(
            decode_words("Élodie", &[(0, 2), (2, 7)], &[Some(0), Some(0)], &logits),
            Err(InspectError::ModelUnavailable)
        ));
        Ok(())
    }
}
