import { useEffect, useLayoutEffect, useState } from 'react';

export type Experience = { board: 'auto' | 'flat'; motion: 'system' | 'reduced' };
export const experienceKey = 'fly-experience';
export function parseExperience(raw: string | null): Experience {
  let value: Partial<Experience> = {};
  try { value = JSON.parse(raw || '{}') || {}; } catch { /* Use defaults for old or damaged preferences. */ }
  return { board: value.board === 'flat' ? 'flat' : 'auto', motion: value.motion === 'reduced' ? 'reduced' : 'system' };
}
export function useExperience() {
  const read = () => { try { return parseExperience(localStorage.getItem(experienceKey)); } catch { return parseExperience(null); } };
  const [preferences, setPreferences] = useState(read);
  const [systemReduced, setSystemReduced] = useState(() => matchMedia('(prefers-reduced-motion: reduce)').matches);
  useEffect(() => {
    const media = matchMedia('(prefers-reduced-motion: reduce)');
    const update = () => setSystemReduced(media.matches);
    media.addEventListener('change', update);
    const sync = (e: StorageEvent) => { if (e.key === experienceKey || e.key === null) setPreferences(read()); };
    addEventListener('storage', sync);
    return () => { media.removeEventListener('change', update); removeEventListener('storage', sync); };
  }, []);
  const reducedMotion = preferences.motion === 'reduced' || systemReduced;
  useLayoutEffect(() => { document.documentElement.dataset.motion = reducedMotion ? 'reduce' : 'normal'; }, [reducedMotion]);
  const change = (next: Experience) => {
    const safe = parseExperience(JSON.stringify(next));
    setPreferences(safe);
    try { localStorage.setItem(experienceKey, JSON.stringify(safe)); } catch { /* Settings still work for this visit. */ }
  };
  return { preferences, change, reducedMotion, systemReduced };
}
