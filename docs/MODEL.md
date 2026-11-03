# General NER model

The optional model is `Xenova/bert-base-NER`, revision `24c7e5aba9ae350923357a6f0b92571be34037ec`, using the existing INT8 ONNX artifact. The source is `dslim/bert-base-NER`. Model metadata declares MIT licensing; retain source/model-card notices when redistributing artifacts.

- [Pinned converted model](https://huggingface.co/Xenova/bert-base-NER/tree/24c7e5aba9ae350923357a6f0b92571be34037ec)
- [Source model card](https://huggingface.co/dslim/bert-base-NER)
- [Authoritative checksums and provenance manifest](../models/general-ner.json)

`node --no-warnings --experimental-strip-types scripts/fetch-model.ts` downloads and verifies all three SHA-256 digests before replacing files. The INT8 model is 108952255 bytes. Startup independently verifies the model and tokenizer. Downloaded weights are ignored by Git and never edited as text.

Runtime: `ort =2.0.0-rc.10`, Level3 optimization, a bounded startup-loaded session pool and one shared `Arc<Tokenizer>`. Defaults remain one session, four intra-op threads and spinning enabled. Session count and threads each accept 1/2/4, with a combined budget of at most eight; sessions are leased exclusively and input buffers are cleared before reuse. See [runtime configuration](OPERATIONS.md#configuration). Nine BIO labels are pinned. PERSON, ORGANIZATION and LOCATION use a 0.5 confidence threshold and original UTF-8 byte offsets. Contiguous subtokens with the same tokenizer word ID are grouped before BIO span assembly: the first confident supported entity piece selects the word's label, and the whole tokenizer word is masked. A low-confidence or outside-label suffix cannot leave part of a detected word exposed. Wholly uncertain words are not promoted. MISC is not a contextual masking category. Overlapping rule/NER spans are merged to cover their union.

Rules run first. Contextual policy triggers NER unless already blocked, including when rules found nothing. More than 512 tokenizer tokens is rejected; silent truncation is disabled and long-input chunking is not implemented. A missing model fails startup when enabled; requiring contextual detection in rules-only mode fails closed.

The synthetic regression fixture contains three English sentences with five exact labeled entities and five matches. Public service tests verify restoration, whole-word masking and token-limit rejection. This tiny fixture is separate from corpus acceptance. Turkish/domain accuracy is unverified; production continues to use the pinned upstream INT8 artifact.

## Alternative models and Turkish support

Researched: 2026-10-08. Turkish is an intended use case for future model evaluation. Publisher model cards, current repository metadata/configuration and artifact listings were reviewed. The candidates below can be considered for Turkish support; this note does not establish their acceptance in Prompt Shield. Published metrics use different corpora and scoring methods and must not be ranked against this project's English scores.

| Candidate | Scope and declared license | Evaluation considerations |
| --- | --- | --- |
| [nym-pii-multilingual-small](https://huggingface.co/Wismut/nym-pii-multilingual-small) | Multilingual PII; Turkish is explicitly listed. MIT. | 40 entity types / 81 BIO labels. Ships FP32 and compressed ONNX variants. The publisher describes the `int8/` variant as approximately 133 MB; it uses embedding INT8 and FP16 body weights with FP32 compute. Use its matching pruned tokenizer. The smaller `edge-int8/` variant reports greater out-of-distribution quality loss. No Prompt Shield or Turkish-specific acceptance result exists here. |
| [Hushmark-TR 289M](https://huggingface.co/lokomotifai/hushmark-tr-289m) | Turkish contextual personal-data detection; 12 categories. Apache-2.0. | Ships an approximately 1.16 GB FP32 ONNX graph and checksum manifest. Training/evaluation are synthetic and share a template family; published high F1 is not evidence for arbitrary Turkish prompts. Its experimental INT8 export was rejected. Effective input limit is 384 tokens, requiring an explicit contract decision before integration. |
| [Turkish mDeBERTa NER](https://huggingface.co/akdeniz27/mDeBERTa-v3-base-turkish-ner) | Turkish PERSON, ORGANIZATION and LOCATION. MIT. | Seven BIO labels, with a different label order from the current model. Useful as a contextual Turkish comparison; the reviewed repository does not ship an ONNX graph. Conversion, quantization and byte-offset restoration need verification. |
| [Turkish PII Detection](https://huggingface.co/isakulaksiz/turkish-pii-detection) | Turkish BERT NER. MIT. | Despite the PII name, its configuration exposes only PERSON, ORGANIZATION and LOCATION through seven BIO labels. Do not infer coverage of IBAN, national IDs or secrets from the repository name. No ONNX graph or scored acceptance report was found in the reviewed repository. |
| [GLiNER Multi PII](https://huggingface.co/urchade/gliner_multi_pii-v1) | PII-focused multilingual candidate. Apache-2.0. | An [ONNX conversion](https://huggingface.co/onnx-community/gliner_multi_pii-v1/tree/main/onnx) is available. Evaluate Turkish detection explicitly; multilingual metadata alone does not prove Turkish coverage. |

Initial shortlist: nym-small for multilingual PII, with Turkish mDeBERTa as the narrower contextual Turkish comparison. Keep the accepted production model as the baseline. Testing every researched model is unnecessary: run protection/offset/restoration quality checks on these two candidates first, and measure resources/latency only for candidates that pass. Hushmark-TR is a second-stage option if broader Turkish contextual categories are required or the initial shortlist is unsuitable. This is an evaluation choice, not a verified replacement winner. The [larger nym model](https://huggingface.co/Wismut/nym-pii-multilingual) can serve as a teacher/reference comparison. Artifact size is not a runtime RAM or latency measurement.

Other researched models are retained with specific limits:

| Model | Reason to defer or exclude from the initial comparison |
| --- | --- |
| [NewMind Turkish GLiNER](https://huggingface.co/newmindai/gliner-multi-v2.1-nemotron-pii-tr) | Apache-2.0; trained on English-to-Turkish translated Nemotron PII. The publisher reports ONNX F1 0.5919 but quantized ONNX F1 0.1127 and recall 0.0603 on its own test set. Do not adopt that quantized artifact as a lighter replacement. |
| [GLiNER PAM PII Large](https://huggingface.co/omeryentur/gliner-pam-pii-large) | Apache-2.0; Turkish/English PAM and support-ticket specialization. Ships an approximately 1.78 GB FP32 ONNX graph. Its publisher reports slow CPU ONNX execution and failed quantization; evaluation is in-domain and training is mostly synthetic. A domain-specific option, not the initial general gateway candidate. |
| [OpenMed Turkish SuperClinical Small](https://huggingface.co/OpenMed/OpenMed-PII-Turkish-SuperClinical-Small-44M-v1) and [LiteClinical Small](https://huggingface.co/OpenMed/OpenMed-PII-Turkish-LiteClinical-Small-66M-v1) | Apache-2.0 and Turkish-targeted PII/PHI checkpoints. Their current cards explicitly state that no verified Turkish evaluation artifact was available. Older indexed LiteClinical card text describes Arabic; do not reuse those scores as Turkish evidence. Hold until representative Turkish evaluation is available. |
| [HikmaAI multilingual PII](https://huggingface.co/HikmaAI/hikmaai-mdeberta-v3-base-pii-multilingual) | Apache-2.0 and ONNX variants, but Turkish is absent from its declared 11-language scope. Not selected for the initial Turkish comparison. |

None of these new candidates has been integrated or benchmarked in this project, and no model weights were downloaded during this research. Before adoption, verify licensing/provenance, pin artifact checksums, and validate ONNX inputs, tokenizer offsets and entity decoding. GLiNER requires integration work beyond replacing the existing model file; expanded BIO taxonomies also require an explicit policy/label mapping. Preserve the rules-first path and current fail-closed token-limit behavior rather than silently truncating inputs to a candidate's shorter limit.

Evaluate held-out English and Turkish examples through the real inspection/restoration boundary, including Turkish characters and names, whole-word masking, missed sensitive spans and unnecessary masking. Record RAM and latency alongside detection quality. The short-NER P99 <50 ms goal is advisory, not a mandatory release, acceptance or model-promotion gate; the [existing protection requirements](#smaller-model-protection-gate) still apply when replacing an accepted model. The two previously rejected DistilBERT candidates are documented below and remain rejected.

### Commercial and proprietary use

Reviewed: 2026-10-08. [MIT](https://spdx.org/licenses/MIT.html) permits commercial use, modification and redistribution without requiring the application to publish its source; preserve copyright and license notices. [Apache-2.0](https://www.apache.org/licenses/LICENSE-2.0) also permits proprietary commercial use; retain the license and applicable attribution/NOTICE material, identify modified files when distributing them, and respect its patent/trademark provisions.

| Candidate | Model/base license evidence | Current licensing assessment |
| --- | --- | --- |
| nym-small | Publisher declares MIT; [mmBERT-small](https://huggingface.co/jhu-clsp/mmBERT-small) also declares MIT. | Compatible in principle with a proprietary commercial application under those published terms. Preserve the model-card/license snapshot; the reviewed model repository has no separate LICENSE file. Training includes machine-labeled Wikipedia text; dataset/provenance review is separate from the model's license declaration, and redistributing training text requires its own rights. |
| Turkish mDeBERTa | Publisher declares MIT; [Microsoft mDeBERTa](https://huggingface.co/microsoft/mdeberta-v3-base) also declares MIT. | Compatible in principle under the published MIT terms. Preserve the model-card/license snapshot; the reviewed fine-tuned model repository has no separate LICENSE file. |
| Hushmark-TR | Ships [Apache-2.0 LICENSE](https://huggingface.co/lokomotifai/hushmark-tr-289m/blob/main/LICENSE), NOTICE and [third-party notices](https://huggingface.co/lokomotifai/hushmark-tr-289m/blob/main/THIRD_PARTY_NOTICES.md) for Apache/MIT upstream components. | Compatible in principle under those terms; retain the supplied notices when redistributing artifacts. |

Model weights, runtime code and training data have distinct licensing scopes. The reviewed [nym runtime](https://github.com/byteowlz/nym/blob/main/LICENSE) is MIT and [GLiNER runtime](https://github.com/urchade/GLiNER/blob/main/LICENSE) is Apache-2.0; check the exact versions and dependencies actually shipped. A permissive model license does not replace Prompt Shield's own [dual-licensing terms](../LICENSE), Enterprise hosted-service terms (see `LICENSE` in `prompt-shield-enterprise`), or shared-core ownership/contribution rights. These findings record published license evidence; they do not certify the complete product's rights chain.

## Current CI accuracy gate

This historical score snapshot is retained for its named revision and includes the session-pool implementation. The later workflow and its scope are recorded in [current CI verification](IMPLEMENTATION_STATUS.md#recorded-ci-verification); unavailable later artifacts must not be replaced with inferred numerical scores. The separate smaller-model and disjoint confirmation evaluations below remain local evidence; this workflow evaluates the pinned BERT artifacts on the primary corpus.

[Actions run 34062487943](https://github.com/dQqAn/prompt-shield/actions/runs/34062487943) passed on commit `c5daba353a56740663264a2648745a78006c0b1f`. All 2,000 pinned English examples were evaluated without token-limit rejection. Aggregate scores match the previous successful CI run.

| Artifact | Exact-span F1 | Recall | Relative gate |
| --- | ---: | ---: | --- |
| FP32 | 69.78% | 76.06% | Reference |
| Existing INT8 | 69.40% | 75.73% | Passed |
| Generated INT8 | 69.24% | 75.62% | Passed |

Both INT8 variants meet the unchanged maximum one-point overall F1/recall loss and two-point per-class recall loss. Two conversions produced identical hashes and repeated candidate predictions matched. [Retained CI scores and identities](evidence/ci-34062487943.json).

The local results below predate this CI measurement. INT8 exact counts differ slightly between environments; compare variants within the same run rather than substituting a local baseline. The disjoint local confirmation sample was not part of this CI job.

## Smaller-model protection gate

The manual performance phase evaluated the pinned dslim and ONNX Community DistilBERT models in [performance-candidates.json](../models/performance-candidates.json). These are distinct from the generated BERT INT8 conversion in the CI table. Both smaller models were converted twice with identical hashes and predictions, using their verified input schemas and BIO label order with the production Rust decoder.

Each failed the relative quantization-loss gate and the stricter comparison with production protection on the original 2,000 examples and a disjoint 2,000-example confirmation corpus. The production comparison requires no overall or per-class precision/recall/F1 regression, no newly exposed previously protected sensitive characters, and no increase in non-sensitive masking. They were disqualified before performance-based selection; production still accepts only the original pinned model. Scores, masking deltas and conversion evidence are in [the performance report](PERFORMANCE_RESULTS.md#smaller-models).

## Historical accuracy failure

The separately locked conversion/evaluation pipeline is documented in [verification commands](VERIFICATION.md). The following failure records the earlier decoder, not current acceptance.

Before the wordpiece fix, on 2,000 pinned Few-NERD English test examples, FP32 exact-span F1/recall were 54.72%/63.98%; existing INT8 52.59%/62.12%; generated INT8 52.29%/61.68%. Neither INT8 variant passed the maximum 1-percentage-point overall F1/recall drop. Existing INT8 also exceeded the 2-point ORGANIZATION recall drop; the candidate exceeded both ORGANIZATION and LOCATION limits.

All 2,000 samples were evaluated without token-limit rejection. Repeated conversion produced the same SHA-256 (99392401feb12b98459e795f7aca75786d52c099e333b29d272fe71d362a7e2c) and identical predictions. Corpus selection, label mapping and cross-dataset limitations are documented in VERIFICATION.md. These scores do not establish Turkish or domain accuracy.

The earlier comparison exited nonzero and retained its failure report. [Historical counts, scores and artifact identities](evidence/model-evaluation-2026-09-05.json).

## Wordpiece masking correction

The failed run exposed a decoding issue: individual entity subtokens were emitted or discarded independently, and repeated BIO beginnings within a word could fragment the entity. Quantization changes in suffix predictions magnified this problem. The `first_confident_entity_wordpiece_v1` decoder groups only the word IDs supplied by the pinned tokenizer; it does not expand across arbitrary whitespace or change the model, tokenizer, confidence threshold or accuracy limits. Evaluation records its decoder version and rejects mixed-version comparisons.

On the same 2,000 examples, the corrected Windows evaluator reports FP32 F1/recall 69.78%/76.06%, production INT8 69.38%/75.75%, and generated INT8 69.41%/75.67%. Both INT8 artifacts pass the original overall and class-specific relative-accuracy limits. FP32 is rerun with the same decoder, so this is not a comparison against an obsolete baseline. A disjoint confirmation sample (positions 2001-4000 in the same deterministic corpus ordering) also passes without further parameter tuning.

A before/after replay of all 2,000 examples through the actual production gRPC service fully covers 269 additional annotated entities and 949 additional non-whitespace reference characters, without uncovering any previously masked reference character. It also masks 735 additional characters outside the supported reference annotations; whole-word protection can increase over-masking. All 4,000 post-fix restoration calls reproduced their original inputs. This fixes partial-word exposure and the measured quantization regression, not every missed entity or overall model accuracy. See [validation evidence](evidence/ner-wordpiece-fix.json).
