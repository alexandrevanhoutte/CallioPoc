// A small RMS gate. Gemini still owns automatic speech detection.
const CHUNK_MS = 20;

export class VoiceActivityGate {
  constructor({
    startThreshold = 0.008,
    endThreshold = 0.005,
    preRollMs = 200,
    hangoverMs = 700,
    startFrames = 2,
  } = {}) {
    this.startThreshold = startThreshold;
    this.endThreshold = endThreshold;
    this.preRollFrames = Math.round(preRollMs / CHUNK_MS);
    this.hangoverFrames = Math.round(hangoverMs / CHUNK_MS);
    this.startFrames = startFrames;
    this.reset();
  }

  reset() {
    this.state = "idle";
    this.preRoll = [];
    this.startCount = 0;
    this.quietCount = 0;
    this.processed = 0;
    this.filtered = 0;
  }

  // Each frame is an independent Int16Array from the microphone worklet.
  // Returned frames must be sent before the optional end-of-stream control.
  push(frame, rms) {
    this.processed++;

    if (this.state === "idle") {
      this.preRoll.push(frame);
      if (this.preRoll.length > this.preRollFrames) {
        this.preRoll.shift();
        this.filtered++;
      }
      this.startCount = rms >= this.startThreshold ? this.startCount + 1 : 0;
      if (this.startCount < this.startFrames) return { frames: [], end: false };

      this.state = "speaking";
      this.startCount = 0;
      this.quietCount = 0;
      const frames = this.preRoll;
      this.preRoll = [];
      return { frames, end: false };
    }

    // Hysteresis preserves quieter syllables within an utterance.
    this.quietCount = rms < this.endThreshold ? this.quietCount + 1 : 0;
    if (this.quietCount < this.hangoverFrames) {
      return { frames: [frame], end: false };
    }

    this.state = "idle";
    this.quietCount = 0;
    this.startCount = 0;
    return { frames: [frame], end: true };
  }
}
