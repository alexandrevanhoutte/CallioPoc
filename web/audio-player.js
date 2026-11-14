// Schedule PCM16 chunks without discarding short bursts from Gemini Live.
// 20 seconds of 24kHz mono PCM16 is ~960 KiB, so this queue is still bounded.
const MAX_SCHEDULED_AUDIO_SECONDS = 20;
const START_DELAY_SECONDS = 0.04;

export class AudioPlayer {
  constructor() {
    this.context = null;
    this.nextStart = 0;
    this.sources = new Set();
    this.onIdle = null;
  }

  async enable() {
    if (this.context) return;
    const context = new AudioContext();
    this.context = context;
    this.nextStart = context.currentTime;
    try {
      await context.resume();
    } catch (error) {
      await this.disable();
      throw error;
    }
  }

  play(buffer, sampleRate = 16000) {
    const context = this.context;
    if (!context || context.state !== "running") return false;
    if (!(buffer instanceof ArrayBuffer) || buffer.byteLength < 2 || buffer.byteLength % 2 !== 0) return false;
    if (sampleRate !== 16000 && sampleRate !== 24000) return false;

    const duration = buffer.byteLength / 2 / sampleRate;
    const queued = Math.max(0, this.nextStart - context.currentTime);
    // Unlike the former 250ms limit, this keeps complete short responses even
    // when Gemini sends several seconds of audio faster than real time.
    if (queued + duration > MAX_SCHEDULED_AUDIO_SECONDS) return false;

    const samples = new DataView(buffer);
    const audio = context.createBuffer(1, buffer.byteLength / 2, sampleRate);
    const channel = audio.getChannelData(0);
    for (let i = 0; i < channel.length; i++) {
      channel[i] = samples.getInt16(i * 2, true) / 32768;
    }

    const source = context.createBufferSource();
    source.buffer = audio;
    source.connect(context.destination);
    source.onended = () => {
      this.sources.delete(source);
      source.disconnect();
      if (this.sources.size === 0) this.onIdle?.();
    };
    this.sources.add(source);
    const startAt = Math.max(context.currentTime + START_DELAY_SECONDS, this.nextStart);
    source.start(startAt);
    this.nextStart = startAt + audio.duration;
    return true;
  }

  // A model interruption must discard all pending speech immediately.
  clear() {
    for (const source of this.sources) {
      source.onended = null;
      try { source.stop(); } catch { /* Already finished. */ }
      source.disconnect();
    }
    this.sources.clear();
    if (this.context) this.nextStart = this.context.currentTime;
  }

  async disable() {
    const context = this.context;
    this.context = null;
    this.clear();
    this.nextStart = 0;
    if (context && context.state !== "closed") await context.close();
  }
}
