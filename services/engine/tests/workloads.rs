#![cfg(feature = "evaluation")]

#[test]
#[ignore = "requires pinned candidate tokenizer downloads"]
fn candidate_tokenizer_truncation_does_not_hide_the_513_token_boundary()
-> Result<(), Box<dyn std::error::Error>> {
    for candidate in ["dslim-distilbert", "community-distilbert"] {
        let output = std::process::Command::new(env!("CARGO_BIN_EXE_ner-evaluate"))
            .args([
                "workloads",
                &format!("../../output/performance-candidates/{candidate}/tokenizer.json"),
            ])
            .output()?;
        assert!(output.status.success());
        let cases: Vec<serde_json::Value> = serde_json::from_slice(&output.stdout)?;
        for length in [32, 128, 512, 513] {
            let selected: Vec<_> = cases
                .iter()
                .filter(|row| row["name"] == format!("ner-{length}"))
                .collect();
            assert_eq!(selected.len(), 8);
            assert!(selected.iter().all(|row| row["tokens"] == length));
        }
    }
    Ok(())
}
