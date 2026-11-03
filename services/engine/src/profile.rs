//! Compile-time opt-in timing. Only stage names and durations leave the process.
pub(crate) struct Stage {
    #[cfg(feature = "profiling")]
    name: &'static str,
    #[cfg(feature = "profiling")]
    started: std::time::Instant,
}

impl Stage {
    pub(crate) fn new(_name: &'static str) -> Self {
        Self {
            #[cfg(feature = "profiling")]
            name: _name,
            #[cfg(feature = "profiling")]
            started: std::time::Instant::now(),
        }
    }
}

impl Drop for Stage {
    fn drop(&mut self) {
        #[cfg(feature = "profiling")]
        eprintln!(
            "profile stage={} nanos={}",
            self.name,
            self.started.elapsed().as_nanos()
        );
    }
}
