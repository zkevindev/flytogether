import { useEffect, useRef, useState } from 'react';
import type { Game } from '../lib/types';
import { announcementKey, Narrator } from '../lib/narration';

type Speech = { key: string; text: string; audio: string; sample_rate: number };
export function HostNarration({ roomID, game, enabled, narrator, onText }: {
  roomID: string; game: Game; enabled: boolean; narrator: Narrator; onText: (text: string) => void;
}) {
  const key = announcementKey(game);
  const [status, setStatus] = useState('');
  const [retry, setRetry] = useState(0);
  const spoken = useRef('');
  const pending = useRef<Speech | undefined>(undefined);
  const generation = useRef(0);
  const play = async (value: Speech, epoch: number) => {
    if (epoch !== generation.current) return;
    const playing = await narrator.playPCM(value.audio, value.sample_rate, () => { if (generation.current === epoch) setStatus(''); });
    if (epoch !== generation.current) return;
    if (playing) { spoken.current = value.key; pending.current = undefined; onText(value.text); setStatus('灵矽正在播报回合'); }
    else { pending.current = value; setStatus('点击启用播报声音'); }
  };
  useEffect(() => {
    const epoch = ++generation.current;
    const controller = new AbortController();
    pending.current = undefined; narrator.stop(); setStatus('');
    if (!enabled || !key || spoken.current === key) return;
    setStatus('灵矽正在准备回合播报');
    void (async () => {
      try {
        const request = () => fetch('/api/rooms/' + encodeURIComponent(roomID) + '/announcement', {
          method: 'POST', headers: { 'Content-Type': 'application/json' },
          body: JSON.stringify({ game_id: game.id, key }), signal: AbortSignal.any([controller.signal, AbortSignal.timeout(16000)]),
        });
        let response = await request();
        if (response.status === 429) { await new Promise(resolve => setTimeout(resolve, 300)); if (controller.signal.aborted) return; response = await request(); }
        if (response.status === 409) { if (epoch === generation.current) setStatus(''); return; }
        if (!response.ok) throw new Error('灵矽播报暂时不可用');
        const value: Speech = await response.json();
        if (value.key !== key || controller.signal.aborted || epoch !== generation.current) return;
        await play(value, epoch);
      } catch { if (!controller.signal.aborted && epoch === generation.current) setStatus('重试灵矽回合播报'); }
    })();
    return () => { generation.current++; controller.abort(); narrator.stop(); };
  }, [enabled, key, roomID, narrator, onText, retry]);
  if (!enabled || !status) return null;
  if (status.startsWith('点击')) return <button className="narration-action" onClick={() => { const value = pending.current; void narrator.unlock(); if (value) void play(value, generation.current); }}>{status}</button>;
  if (status.startsWith('重试')) return <button className="narration-action" onClick={() => { void narrator.unlock(); setRetry(value => value + 1); }}>{status}</button>;
  return <p className="narration-status" role="status">{status}</p>;
}
