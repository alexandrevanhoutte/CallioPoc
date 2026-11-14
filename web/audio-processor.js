// AudioWorklet: turn microphone samples into 20 ms mono PCM16 chunks at 16 kHz.
const OUTPUT_RATE = 16000;
const CHUNK_SIZE = 320;

class PCM16Processor extends AudioWorkletProcessor {
  constructor() {
    super();
    this.samplesPerOutput = sampleRate / OUTPUT_RATE;
    this.remaining = this.samplesPerOutput;
    this.weightedSum = 0;
    this.chunk = new Int16Array(CHUNK_SIZE);
    this.chunkOffset = 0;
    this.energy = 0;
  }

  process(inputs, outputs) {
    // Keep the processing graph alive without playing microphone audio.
    for (const output of outputs[0] ?? []) output.fill(0);

    const input = inputs[0]?.[0];
    if (!input) return true;

    // Average across source-sample intervals (also handles 44.1 kHz -> 16 kHz).
    for (const sample of input) {
      let unused = 1;
      while (unused > 1e-10) {
        const portion = Math.min(unused, this.remaining);
        this.weightedSum += sample * portion;
        this.remaining -= portion;
        unused -= portion;

        if (this.remaining <= 1e-10) {
          this.addSample(this.weightedSum / this.samplesPerOutput);
          this.weightedSum = 0;
          this.remaining = this.samplesPerOutput;
        }
      }
    }

    return true;
  }

  addSample(sample) {
    const clamped = Math.max(-1, Math.min(1, sample));
    this.chunk[this.chunkOffset++] = clamped < 0
      ? Math.round(clamped * 32768)
      : Math.round(clamped * 32767);
    this.energy += clamped * clamped;

    if (this.chunkOffset === CHUNK_SIZE) {
      const buffer = this.chunk.buffer;
      const rms = Math.sqrt(this.energy / CHUNK_SIZE);
      this.port.postMessage({ type: "audio", buffer, rms }, [buffer]);

      this.chunk = new Int16Array(CHUNK_SIZE);
      this.chunkOffset = 0;
      this.energy = 0;
    }
  }
}

registerProcessor("pcm16-recorder", PCM16Processor);
