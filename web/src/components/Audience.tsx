import { useState } from 'react';
import type { Room } from '../lib/types';
import { api } from '../lib/types';

export const cheers = [
  { emoji: '💪', label: '加油' },
  { emoji: '👏', label: '鼓掌' },
  { emoji: '✨', label: '漂亮' },
  { emoji: '🔥', label: '好险' },
  { emoji: '💛', label: '冲呀' },
  { emoji: '😄', label: '开心' },
];

function spokenAt(at: string) {
  const date = new Date(at);
  if (Number.isNaN(date.getTime())) return '';
  return date.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit' });
}

export function AudienceHost({ room, onRoom }: { room: Room; onRoom: (room: Room) => void }) {
  const audience = room.audience;
  const [error, setError] = useState('');
  const [busy, setBusy] = useState(false);
  if (!room.network || !room.me?.host || !audience) return null;
  const link = audience.token ? location.origin + '/#watch=' + room.id + '.' + audience.token : '';
  const screenLink = audience.token ? location.origin + '/#screen=' + room.id + '.' + audience.token : '';
  const update = async (body: { enabled?: boolean; rotate?: boolean; chat_enabled?: boolean }) => {
    setBusy(true); setError('');
    try { onRoom(await api<Room>('/api/rooms/' + room.id + '/audience', body)); }
    catch (e) { setError(e instanceof Error ? e.message : '观赛设置没有保存'); }
    finally { setBusy(false); }
  };
  return <section className="audience-host" aria-label="观赛设置">
    <div><strong>家人观赛</strong><span>{audience.enabled ? `${audience.online} 人正在看 · 已加入 ${audience.count}/${audience.capacity}` : '观赛已关闭'}</span></div>
    <p>观众不占席位，不能掷骰或走棋。多个标签页仍算同一位观众；退出只移除自己。</p>
    <div className="lobby-actions">
      <button type="button" className="outline-button" disabled={busy} onClick={() => void update({ enabled: !audience.enabled })}>{audience.enabled ? '关闭观赛' : '允许观赛'}</button>
      {audience.enabled && link && <button type="button" className="outline-button" onClick={() => void navigator.clipboard.writeText(link).catch(() => setError('复制失败，请手动选择链接'))}>复制观赛链接</button>}
      {audience.enabled && screenLink && <button type="button" className="outline-button" onClick={() => void navigator.clipboard.writeText(screenLink).catch(() => setError('复制失败，请手动选择链接'))}>复制电视大屏链接</button>}
      {audience.enabled && <button type="button" className="text-button" disabled={busy} onClick={() => void update({ rotate: true })}>更新链接</button>}
      {audience.enabled && <button type="button" className="text-button" disabled={busy} onClick={() => void update({ chat_enabled: !audience.chat_enabled })}>{audience.chat_enabled ? '禁止观众发言' : '允许观众发言'}</button>}
    </div>
    {audience.enabled && link && <input aria-label="观赛链接" className="room-name-input" readOnly value={link} onFocus={e => e.target.select()} />}
    {audience.enabled && screenLink && <input aria-label="电视大屏链接" className="room-name-input" readOnly value={screenLink} onFocus={e => e.target.select()} />}
    {error && <p role="alert">{error}</p>}
  </section>;
}

export function AudienceChat({ room, onRoom, players }: { room: Room; onRoom: (room: Room) => void; players: { id: string; name: string; ai?: boolean }[] }) {
  const [hidden, setHidden] = useState(() => localStorage.getItem('fly-chat-hidden') === 'on');
  const [text, setText] = useState('');
  const [target, setTarget] = useState('');
  const [error, setError] = useState('');
  const audience = room.audience;
  if (!audience) return null;
  const mine = audience.me?.id || room.me?.player_id || '';
  const send = async (body: { kind: 'emoji' | 'text'; emoji?: string; text?: string }) => {
    setError('');
    try {
      onRoom(await api<Room>('/api/rooms/' + room.id + '/chat', { id: crypto.randomUUID(), target_player_id: target || undefined, ...body }));
      setText('');
    } catch (e) { setError(e instanceof Error ? e.message : '没有发送出去'); }
  };
  const block = async (fromID: string) => {
    try { onRoom(await api<Room>('/api/rooms/' + room.id + '/chat/block', { from_id: fromID, blocked: true })); }
    catch (e) { setError(e instanceof Error ? e.message : '没有屏蔽成功'); }
  };
  const canSpeak = room.status !== 'ended' && (!!room.me || (audience.me && audience.chat_enabled));
  return <section className="audience-chat" aria-label="观赛互动">
    <div className="audience-chat-head"><strong>互动</strong><span>{audience.online} 人正在看</span><button type="button" className="text-button" onClick={() => { const next = !hidden; setHidden(next); localStorage.setItem('fly-chat-hidden', next ? 'on' : 'off'); }}>{hidden ? '显示消息' : '隐藏消息'}</button></div>
    {hidden ? <p>消息已隐藏。这不影响棋局，也不关闭你的私人灵矽。</p> : <>
      <p>这里只显示发出的互动，不含棋步，也不含对灵矽说的话。</p>
      <ol>{(audience.messages || []).map((message, index, all) => <li key={message.id} aria-live={index === all.length - 1 ? 'polite' : undefined}><span>{message.role === 'spectator' ? '观众' : message.role === 'host' ? '房主' : '选手'} · {message.name} · <time dateTime={message.at}>{spokenAt(message.at)}</time></span><p>{message.kind === 'emoji' ? message.emoji : message.text}{message.target_player_id ? ` · 给${players.find(p => p.id === message.target_player_id)?.name || '选手'}` : ''}</p>{message.from_id !== mine && <button type="button" className="text-button" onClick={() => void block(message.from_id)}>屏蔽</button>}</li>)}</ol>
      {(audience.messages || []).length === 0 && <p>还没有互动。表情和文字不会变成走棋指令。</p>}
    </>}
    {canSpeak && <form onSubmit={e => { e.preventDefault(); if (text.trim()) void send({ kind: 'text', text }); }}>
      <div className="cheer-row">{cheers.map(item => <button type="button" key={item.emoji} aria-label={item.label} onClick={() => void send({ kind: 'emoji', emoji: item.emoji })}>{item.emoji}<span>{item.label}</span></button>)}</div>
      <label className="field-label" htmlFor={'cheer-target-' + room.id}>给谁加油，可选</label>
      <select id={'cheer-target-' + room.id} value={target} onChange={e => setTarget(e.target.value)}><option value="">不指定选手</option>{players.filter(p => !p.ai).map(p => <option key={p.id} value={p.id}>{p.name}</option>)}</select>
      <label className="field-label" htmlFor={'cheer-text-' + room.id}>短消息</label>
      <div className="chat-compose"><input id={'cheer-text-' + room.id} maxLength={40} value={text} onChange={e => setText(e.target.value)} placeholder="最多 40 字" /><button className="primary-button" type="submit">发送</button></div>
    </form>}
    {audience.me && !audience.chat_enabled && <p>房主已关闭观众发言，你仍可以安静观看，或只问自己的灵矽。</p>}
    {room.status === 'ended' && <p>本局已结束，互动记录保留到房主再玩或删除房间。</p>}
    {error && <p role="alert">{error}</p>}
  </section>;
}
