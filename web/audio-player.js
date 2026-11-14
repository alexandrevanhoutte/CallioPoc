// Small bounded PCM16 playback queue. Disabled by default to prevent feedback.
export class AudioPlayer {
  constructor() {
    this.context = null;
    this.nextStart = 0;
    this.sources = new Set();
  }

  async enable() {
    if (this.context) return;
    // Call directly from the user's checkbox event to satisfy autoplay policies.
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

  play(buffer) {
    const context = this.context;
    if (!context || context.state !== "running" || buffer.byteLength !== 640) return false;
    if (this.nextStart - context.currentTime > 0.25) return false;

    const samples = new DataView(buffer);
    const audio = context.createBuffer(1, 320, 16000);
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
    };
    this.sources.add(source);
    const startAt = Math.max(context.currentTime + 0.04, this.nextStart);
    source.start(startAt);
    this.nextStart = startAt + audio.duration;
    return true;
  }

  async disable() {
    const context = this.context;
    this.context = null;
    this.nextStart = 0;
    for (const source of this.sources) {
      source.onended = null;
      try { source.stop(); } catch { /* Already finished. */ }
      source.disconnect();
    }
    this.sources.clear();
    if (context && context.state !== "closed") await context.close();
  }
}
