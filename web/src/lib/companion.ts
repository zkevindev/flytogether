import type { CompanionPreferences } from './types';
export function loadCompanion(): CompanionPreferences {
  try {
    const value = JSON.parse(localStorage.getItem('fly-companion') || '{}');
    return { style: ['warm', 'coach', 'concise'].includes(value?.style) ? value.style : 'warm', frequency: ['normal', 'highlights', 'quiet'].includes(value?.frequency) ? value.frequency : 'normal' };
  } catch { return { style: 'warm', frequency: 'normal' }; }
}
