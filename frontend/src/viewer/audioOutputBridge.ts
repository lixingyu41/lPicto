const bridgeFrequencyHz = 19_000;
const bridgeGain = 0.00025;
const bridgeTimeoutMs = 8_000;
const suspendDelayMs = 250;

type WebkitWindow = Window & typeof globalThis & {
  webkitAudioContext?: typeof AudioContext;
};

class ViewerAudioOutputBridge {
  private activeMediaKey = '';
  private bridgeSourceKey = '';
  private bridgeTargetKey = '';
  private context: AudioContext | null = null;
  private gain: GainNode | null = null;
  private oscillator: OscillatorNode | null = null;
  private suspendTimer = 0;
  private timeoutTimer = 0;

  prime() {
    const context = this.ensureContext();
    if (!context) return;
    window.clearTimeout(this.suspendTimer);
    void context.resume().catch(() => undefined);
  }

  mediaStarted(mediaKey: string) {
    if (!mediaKey) return;
    this.prime();
    this.activeMediaKey = mediaKey;
    if (this.bridgeTargetKey === mediaKey) this.finishBridge();
  }

  mediaStopped(mediaKey: string) {
    if (!mediaKey || this.activeMediaKey !== mediaKey) return;
    this.activeMediaKey = '';
    if (this.bridgeSourceKey === mediaKey && this.bridgeTargetKey) return;
    this.scheduleSuspend();
  }

  beginTransition(sourceKey: string, targetKey: string) {
    if (!sourceKey || !targetKey || sourceKey === targetKey || this.activeMediaKey !== sourceKey) return;
    const context = this.ensureContext();
    if (!context || !this.gain) return;
    window.clearTimeout(this.suspendTimer);
    window.clearTimeout(this.timeoutTimer);
    this.bridgeSourceKey = sourceKey;
    this.bridgeTargetKey = targetKey;
    void context.resume().catch(() => undefined);
    this.gain.gain.cancelScheduledValues(context.currentTime);
    this.gain.gain.setValueAtTime(bridgeGain, context.currentTime);
    this.timeoutTimer = window.setTimeout(() => this.finishBridge(), bridgeTimeoutMs);
  }

  dispose() {
    window.clearTimeout(this.suspendTimer);
    window.clearTimeout(this.timeoutTimer);
    this.activeMediaKey = '';
    this.bridgeSourceKey = '';
    this.bridgeTargetKey = '';
    this.oscillator?.stop();
    this.oscillator = null;
    this.gain = null;
    if (this.context) void this.context.close().catch(() => undefined);
    this.context = null;
  }

  private ensureContext() {
    if (this.context) return this.context;
    const AudioContextConstructor = window.AudioContext ?? (window as WebkitWindow).webkitAudioContext;
    if (!AudioContextConstructor) return null;
    const context = new AudioContextConstructor();
    const oscillator = context.createOscillator();
    const gain = context.createGain();
    oscillator.type = 'sine';
    oscillator.frequency.value = bridgeFrequencyHz;
    gain.gain.value = 0;
    oscillator.connect(gain);
    gain.connect(context.destination);
    oscillator.start();
    this.context = context;
    this.gain = gain;
    this.oscillator = oscillator;
    return context;
  }

  private finishBridge() {
    window.clearTimeout(this.timeoutTimer);
    this.timeoutTimer = 0;
    if (this.context && this.gain) {
      this.gain.gain.cancelScheduledValues(this.context.currentTime);
      this.gain.gain.setValueAtTime(0, this.context.currentTime);
    }
    this.bridgeSourceKey = '';
    this.bridgeTargetKey = '';
    if (!this.activeMediaKey) this.scheduleSuspend();
  }

  private scheduleSuspend() {
    window.clearTimeout(this.suspendTimer);
    this.suspendTimer = window.setTimeout(() => {
      if (this.activeMediaKey || this.bridgeTargetKey || !this.context) return;
      void this.context.suspend().catch(() => undefined);
    }, suspendDelayMs);
  }
}

export const viewerAudioOutputBridge = new ViewerAudioOutputBridge();
