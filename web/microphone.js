// Browser-only microphone capture; CI-03 will consume the same PCM16 callback.
export class MicrophoneCapture {
  constructor({ onAudioChunk, onError }) {
    this.onAudioChunk = onAudioChunk;
    this.onError = onError;
    this.stream = null;
    this.context = null;
    this.source = null;
    this.processor = null;
    this.mutedOutput = null;
  }

  async start() {
    if (this.context || this.stream) throw new Error("Microphone is already running.");

    if (!navigator.mediaDevices?.getUserMedia) {
      throw new Error("Microphone access requires localhost or HTTPS and a supported browser.");
    }
    if (!window.AudioContext || !window.AudioWorkletNode) {
      throw new Error("AudioWorklet is not supported by this browser.");
    }

    try {
      this.stream = await navigator.mediaDevices.getUserMedia({
        audio: {
          channelCount: { ideal: 1 },
          echoCancellation: true,
          noiseSuppression: true,
          autoGainControl: true,
        },
      });

      this.context = new AudioContext();
      await this.context.audioWorklet.addModule("/audio-processor.js");

      this.source = this.context.createMediaStreamSource(this.stream);
      this.processor = new AudioWorkletNode(this.context, "pcm16-recorder");
      this.processor.port.onmessage = ({ data }) => {
        if (data.type === "audio") {
          this.onAudioChunk(new Int16Array(data.buffer), data.rms);
        }
      };
      this.processor.onprocessorerror = () => {
        this.onError(new Error("Audio processing stopped unexpectedly."));
      };

      // Connecting through a muted gain keeps the worklet running without echo.
      this.mutedOutput = this.context.createGain();
      this.mutedOutput.gain.value = 0;
      this.source.connect(this.processor);
      this.processor.connect(this.mutedOutput);
      this.mutedOutput.connect(this.context.destination);

      for (const track of this.stream.getAudioTracks()) {
        track.onended = () => this.onError(new Error("Microphone disconnected."));
      }

      await this.context.resume();
    } catch (error) {
      await this.stop();
      throw error;
    }
  }

  async stop() {
    // Clear references first so repeated cleanup is safe.
    const { stream, context, source, processor, mutedOutput } = this;
    this.stream = null;
    this.context = null;
    this.source = null;
    this.processor = null;
    this.mutedOutput = null;

    if (processor) {
      processor.port.onmessage = null;
      processor.onprocessorerror = null;
      processor.port.close();
      processor.disconnect();
    }
    source?.disconnect();
    mutedOutput?.disconnect();

    for (const track of stream?.getTracks() ?? []) {
      track.onended = null;
      track.stop();
    }
    if (context && context.state !== "closed") await context.close();
  }
}
