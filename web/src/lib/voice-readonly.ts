const key = 'fly-voice-readonly-seats';

export function loadVoiceSeatModes(storage: Pick<Storage, 'getItem'> = localStorage): Record<number, boolean> {
  try {
    const value = JSON.parse(storage.getItem(key) || '{}');
    return value && typeof value === 'object' && !Array.isArray(value) ? value : {};
  } catch {
    return {};
  }
}

export function saveVoiceSeatMode(seat: number, enabled: boolean, storage: Pick<Storage, 'getItem' | 'setItem'> = localStorage) {
  const next = { ...loadVoiceSeatModes(storage), [seat]: enabled };
  storage.setItem(key, JSON.stringify(next));
  return next;
}
