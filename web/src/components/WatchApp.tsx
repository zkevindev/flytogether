import { lazy, Suspense, useCallback, useEffect, useReducer, useRef, useState } from 'react';
import type { CSSProperties } from 'react';
import { AirplaneTilt, ArrowClockwise, House, Microphone, SpeakerHigh, SpeakerSlash } from '@phosphor-icons/react';
import { BoardLoader } from './BoardLoader';
import { KeyMoveNarration } from './KeyMoveNarration';
import { latestKeyMove } from '../lib/key-move';
import { PlayerMarker } from './PlayerMarker';
import { DiceRollModal } from './DiceRollModal';
import { AudienceChat } from './Audience';
import { useExperience } from '../lib/experience';
import { api, APIError, palette, socketURL } from '../lib/types';
import type { Config, GameEvent, Room } from '../lib/types';
import { VoiceClient } from '../lib/voice';
import { initialPresentation, presentationReducer } from '../lib/presentation';

const PropEffects = lazy(() => import('./PropEffects').then(module => ({ default: module.PropEffects })));

export function WatchApp({ code, onExit, displayOnly = false }: { code: string; onExit: () => void; displayOnly?: boolean }) {
  const { preferences, change, reducedMotion } = useExperience();
  const [room, setRoom] = useState<Room>();
  const [config, setConfig] = useState<Config>();
  const [name, setName] = useState('');
  const [error, setError] = useState('');
  const [seatKept, setSeatKept] = useState(false);
  const [closed, setClosed] = useState('');
  const [connected, setConnected] = useState(false);
  const [busy, setBusy] = useState(false);
  const [voiceState, setVoiceState] = useState('disconnected');
  const [speech, setSpeech] = useState('');
  const [muted, setMuted] = useState(() => localStorage.getItem('fly-master-muted') === 'on');
  const [presentation, present] = useReducer(presentationReducer, initialPresentation);
  const [impact, setImpact] = useState<GameEvent>();
  const roomRef = useRef<Room | undefined>(undefined);
  const voice = useRef<VoiceClient | undefined>(undefined);
  const gameRef = useRef('');
  const closedRef = useRef('');
  const displayJoinAttempt = useRef('');
  roomRef.current = room;
  closedRef.current = closed;
  const dot = code.indexOf('.');
  const roomID = dot > 0 ? code.slice(0, dot) : '';
  const token = dot > 0 ? code.slice(dot + 1) : '';

  const accept = useCallback((next: Room) => {
    setRoom(old => !old || old.id !== next.id || old.seq <= next.seq ? next : old);
    setClosed('');
  }, []);

  useEffect(() => {
    let ignore = false;
    void api<Config>('/api/config').then(value => { if (!ignore) setConfig(value); }).catch(() => {});
    if (!roomID) { setError('观赛链接不完整'); return; }
    void api<Room>('/api/rooms/' + encodeURIComponent(roomID) + '/watch').then(value => { if (!ignore) accept(value); }).catch(e => {
      if (ignore) return;
      if (!(e instanceof APIError) || e.code !== 'NOT_WATCHING') setError(e instanceof Error ? e.message : '暂时无法打开观赛');
    });
    return () => { ignore = true; voice.current?.disconnect(); };
  }, [accept, roomID]);

  useEffect(() => {
    if (!room?.id || closed) return;
    let stopped = false, socket: WebSocket, timer: ReturnType<typeof setTimeout>;
    const connect = () => {
      if (stopped || !navigator.onLine) { setConnected(false); return; }
      let firstSnapshot = true;
      socket = new WebSocket(socketURL('/ws/game?room_id=' + encodeURIComponent(room.id)));
      socket.onopen = () => setConnected(false);
      socket.onmessage = e => {
        try {
          const data = JSON.parse(e.data);
          if (stopped || roomRef.current?.id !== room.id) return;
          if (data.type === 'audience_closed') { closedRef.current = data.message || '观赛已结束'; setClosed(closedRef.current); setConnected(false); voice.current?.disconnect(); socket.close(); return; }
          if (data.type === 'state' && data.room) {
            if (firstSnapshot) { present({ type: 'reset', game: data.room.state }); setImpact(undefined); firstSnapshot = false; }
            accept(data.room); setConnected(true);
          }
        } catch { /* Ignore a damaged frame and keep the last good snapshot. */ }
      };
      socket.onclose = () => { setConnected(false); if (!stopped && !closedRef.current && navigator.onLine) timer = setTimeout(connect, 1800); };
      socket.onerror = () => socket.close();
    };
    connect();
    const offline = () => { clearTimeout(timer); setConnected(false); socket?.close(); voice.current?.disconnect(); setImpact(undefined); };
    const online = () => { clearTimeout(timer); connect(); };
    addEventListener('offline', offline); addEventListener('online', online);
    return () => { stopped = true; clearTimeout(timer); socket?.close(); removeEventListener('offline', offline); removeEventListener('online', online); };
  }, [accept, closed, room?.id]);

  useEffect(() => {
    const id = room?.state.id || '';
    if (gameRef.current && id && gameRef.current !== id) { voice.current?.disconnect(); setSpeech(''); setVoiceState('disconnected'); setError(''); setImpact(undefined); }
    gameRef.current = id;
  }, [room?.state.id]);

  useEffect(() => { if (room) present({ type: 'sync', game: room.state, status: room.status, catchUp: true }); }, [room]);
  useEffect(() => {
    const beat = presentation.active;
    if (beat?.kind !== 'board') return;
    const timer = setTimeout(() => present({ type: 'advance', id: beat.id }), reducedMotion ? 80 : beat.duration);
    return () => clearTimeout(timer);
  }, [presentation.active, reducedMotion]);
  useEffect(() => {
    const effect = presentation.effect;
    if (!effect) return;
    const hit = [...effect.events].reverse().find(event => event.kind === 'prop' || (event.kind === 'moved' && event.text.includes('回机场')));
    setImpact(hit);
  }, [presentation.effect]);
  useEffect(() => {
    if (!impact) return;
    const timer = setTimeout(() => setImpact(undefined), reducedMotion ? 400 : 2600);
    return () => clearTimeout(timer);
  }, [impact, reducedMotion]);
  useEffect(() => { if (room?.status === 'ended') voice.current?.disconnect(); }, [room?.status]);

  const join = async () => {
    setBusy(true); setError(''); setSeatKept(false);
    try { accept(await api<Room>('/api/rooms/' + encodeURIComponent(roomID) + '/watch', { token, name })); }
    catch (e) {
      if (e instanceof APIError && e.code === 'SEAT_KEPT') setSeatKept(true);
      setError(e instanceof Error ? e.message : '加入观赛没有成功');
    } finally { setBusy(false); }
  };
  useEffect(() => {
    if (!displayOnly || !roomID || !token || room?.audience?.me || displayJoinAttempt.current === roomID) return;
    displayJoinAttempt.current = roomID;
    let ignore = false;
    void api<Room>('/api/rooms/' + encodeURIComponent(roomID) + '/watch', { token, name: '家庭大屏' }).then(value => { if (!ignore) accept(value); }).catch(e => {
      if (!ignore) setError(e instanceof Error ? e.message : '大屏连接观赛失败');
    });
    return () => { ignore = true; };
  }, [accept, displayOnly, room?.audience?.me, roomID, token]);
  const leave = async () => {
    voice.current?.disconnect();
    try { await api('/api/rooms/' + roomID + '/watch/leave', {}); } catch { /* Local exit still returns home. */ }
    onExit();
  };
  const ask = async () => {
    if (!room || muted) return;
    setError('');
    voice.current?.disconnect();
    const client = new VoiceClient(room.id, setVoiceState, (text, who) => { if (who !== 'you') setSpeech(text); }, setError, { style: 'warm', frequency: 'quiet' }, next => { if (next.audience?.me) accept(next); }, '/ws/audience-voice');
    voice.current = client;
    try { await client.connect(); } catch (e) { setError(e instanceof Error ? e.message : '观赛助手没有连上，仍可继续观看'); }
  };

  if (!room?.audience?.me) return displayOnly
    ? <div className="living-screen-loading" role="status"><AirplaneTilt size={48} weight="fill" /><h1>正在连接家庭棋盘</h1><p>{error || '请稍候，大屏正在同步当前棋局。'}</p></div>
    : <div className="loading-page join-page"><AirplaneTilt size={48} weight="fill" /><h1>来看这一局</h1><p>填写昵称即可观看，不占比赛席位，也不能替选手操作。</p>{seatKept ? <p role="alert">{error} 请回到自己的对局页面。</p> : <form onSubmit={e => { e.preventDefault(); void join(); }}><label className="field-label" htmlFor="watch-name">你的昵称</label><input id="watch-name" className="room-name-input" maxLength={10} required value={name} onChange={e => setName(e.target.value)} /><button className="primary-button" disabled={busy || !roomID}>开始观赛</button></form>}{error && !seatKept && <p role="alert">{error}</p>}<button className="text-button" onClick={onExit}>返回首页</button></div>;

  const state = displayOnly ? room.state : presentation.game || room.state;
  const current = state.players[state.current];
  const winner = state.winner === 'team' ? { id: 'team', name: '全队' } : state.players.find(player => player.id === state.winner);
  const latestNarrated = latestKeyMove(state);
  const steps = state.history.slice(-6).filter(step => step.id !== latestNarrated?.id);
  const lastRoll = [...state.history].reverse().find(event => event.kind === 'rolled' && event.dice_rolls?.length);
  if (displayOnly) return <main className={'living-room-screen ' + (state.theme_id === 'star-rescue' ? 'theme-star-rescue' : '')} aria-label="家庭大屏棋盘">
    <header className="living-screen-header"><strong>一起飞 <span>家庭大屏</span></strong><span className={connected ? 'screen-live' : 'screen-offline'}>{connected ? '实时同步中' : '正在连接'}</span></header>
    <section className="living-screen-board" aria-label="只读棋盘">
      <div className="living-screen-status"><div className="living-screen-turn"><PlayerMarker index={state.current} /><strong>{state.winner === 'team' ? '星河救援成功' : winner ? `${winner.name}获胜` : `${current.name}的回合`}</strong></div><div className="living-screen-meta"><span>{state.board.name}</span><span>{state.phase === 'move' ? `掷出 ${state.dice} 点` : state.phase === 'finished' ? '本局结束' : lastRoll ? `刚才掷出 ${lastRoll.dice_rolls!.join('、')} 点` : '等待掷骰'}</span><span>{room.status === 'paused' ? '比赛暂停' : room.status === 'waiting' ? '等待开局' : '比赛进行中'}</span></div></div>
      {state.theme_id === 'star-rescue' && <section className="mission-card" aria-label="星际救援任务"><div className="mission-orbit" aria-hidden="true">✦</div><div className="mission-copy"><span>星际救援 · 合作任务</span><strong>{room.state.mission?.title || '护送全员平安返航'}</strong><small>{room.state.mission?.description || '全员合作，把每位飞行员的飞机送达终点。'}</small></div>{room.state.mission && <div className={'mission-progress ' + room.state.mission.status}><strong>{room.state.mission.progress}<i>/</i>{room.state.mission.target}</strong><small>{room.state.mission.status === 'completed' ? '任务完成' : room.state.mission.status === 'incomplete' ? '任务未完成' : '飞行员就位'}</small></div>}</section>}
      <KeyMoveNarration game={state} />
      <div className="living-screen-canvas"><BoardLoader light={preferences.board === 'flat'} reducedMotion={reducedMotion} game={state} disabled onPiece={() => {}} onRoll={() => {}} /></div>
      <ol className="living-screen-steps" aria-label="最近棋步">{steps.slice(-4).map(step => <li key={step.id}>{step.text}</li>)}</ol>
      <div className="living-screen-crew" style={{ '--seats': state.players.length } as CSSProperties}>{state.players.map((player, index) => <div key={player.id} className={state.current === index && state.phase !== 'finished' ? 'current' : ''}><strong><PlayerMarker index={index} />{player.name}{player.ai ? ' · 机器人' : ''}</strong><span>{player.pieces.filter(piece => piece.progress === state.board.finish).length} 架抵达</span></div>)}</div>
    </section>
    {closed && <p className="living-screen-notice" role="status">{closed}</p>}
  </main>;
  return <div className={'app-shell play-shell watch-shell ' + (state.theme_id === 'star-rescue' ? 'theme-star-rescue' : '')}>
    <a className="skip-link" href="#watch-main">跳到棋盘</a>
    <header className="site-header"><button className="brand brand-button" onClick={() => void leave()}><span className="brand-mark"><AirplaneTilt size={26} weight="fill" /></span><span className="brand-name">一起飞<span>观赛</span></span></button>
      <div className="header-actions"><strong className="watch-badge">观赛中</strong><button className="icon-button" aria-label={muted ? '恢复声音' : '静音'} onClick={() => { const next = !muted; setMuted(next); localStorage.setItem('fly-master-muted', next ? 'on' : 'off'); if (next) voice.current?.disconnect(); }}>{muted ? <SpeakerSlash /> : <SpeakerHigh />}</button><button className="text-button" onClick={() => change({ ...preferences, board: preferences.board === 'flat' ? 'auto' : 'flat' })}>{preferences.board === 'flat' ? '立体棋盘' : '轻量棋盘'}</button><button className="text-button" onClick={() => change({ ...preferences, motion: reducedMotion ? 'system' : 'reduced' })}>{reducedMotion ? '恢复动画' : '减少动画'}</button></div>
    </header>
    <main id="watch-main" tabIndex={-1} className="play-main"><div className="play-layout">
      <section className={'board-panel ' + (state.theme_id === 'star-rescue' ? 'theme-star-rescue' : '')} aria-label="观赛棋盘"><div className="board-turnbar"><span style={{ color: palette[current.color] }}><PlayerMarker index={state.current} />{state.winner === 'team' ? '星河救援成功' : winner ? `${winner.name}获胜` : `${current.name}的回合`}</span><strong>{state.board.name}{state.phase === 'move' ? ` · ${state.dice} 点` : ''} · {room.audience.online} 人正在看</strong></div>
        {state.theme_id === 'star-rescue' && <section className="mission-card" aria-label="星际救援任务"><div className="mission-orbit" aria-hidden="true">✦</div><div className="mission-copy"><span>星际救援 · 合作任务</span><strong>{room.state.mission?.title || '护送全员平安返航'}</strong><small>{room.state.mission?.description || '全员合作，把每位飞行员的飞机送达终点。'}</small></div>{room.state.mission && <div className={'mission-progress ' + room.state.mission.status}><strong>{room.state.mission.progress}<i>/</i>{room.state.mission.target}</strong><small>{room.state.mission.status === 'completed' ? '任务完成' : room.state.mission.status === 'incomplete' ? '任务未完成' : '飞行员就位'}</small></div>}</section>}
        {(room.status === 'waiting' || room.status === 'paused') && <div className="game-status-overlay"><strong>{room.status === 'waiting' ? '等待开局' : '比赛暂停'}</strong><p>{room.status === 'waiting' ? '选手入座后由房主开始，你可以先观看准备情况。' : '棋盘保留在原地，继续后会从这里同步。'}</p><button className="outline-button" onClick={() => void leave()}><House />返回首页</button></div>}
        {(winner || room.status === 'ended') && <p className="watch-result" role="status">{state.winner === 'team' ? '星河救援成功，全队共同获胜。' : winner ? `${winner.name}获胜。` : '房主结束了这一局。'}可以查看棋盘和最近棋步，观众不能再开一局。</p>}
        {!connected && !closed && <p role="status" className="connection-hint">正在重新连接观赛，比赛本身不受影响。</p>}
        <KeyMoveNarration game={state} />
        {closed && <p role="alert" className="connection-hint">{closed} <button className="text-button" onClick={() => void leave()}><House />返回首页</button></p>}
        <div className="board-stage">{impact && <Suspense fallback={null}><PropEffects key={impact.id} event={impact} game={state} light={preferences.board === 'flat'} reducedMotion={reducedMotion} /></Suspense>}<BoardLoader light={preferences.board === 'flat'} reducedMotion={reducedMotion} game={state} disabled onPiece={() => {}} onRoll={() => {}} /></div>
        <ol className="watch-steps" aria-label="最近棋步">{steps.map(step => <li key={step.id}>{step.text}</li>)}</ol>
        <details className="keyboard-help"><summary>键盘操作</summary><p>Tab 在提问、表情和短消息之间移动，Enter 发送。空格和数字不会掷骰或选飞机。Esc 可关闭骰子提示。</p></details>
        <div className="crew-list">{state.players.map((player, index) => <div key={player.id} className={'crew-seat ' + (state.current === index && state.phase !== 'finished' ? 'current' : '')}><strong><PlayerMarker index={index} />{player.name}{player.ai ? ' · 机器人' : ''}{room.network?.seats[index]?.host ? ' · 房主' : ''}</strong><span>{player.pieces.filter(piece => piece.progress === state.board.finish).length} 架抵达</span></div>)}</div>
      </section>
      <aside className="side-column">
        <section className="voice-panel" aria-label="我的灵矽"><h2>问我的灵矽</h2><p>只回答你本人，不广播给选手，也不能替他们操作。</p><div className="voice-bubble" aria-live="polite">{speech || '可以问现在轮到谁、谁更接近终点，或刚才为什么被送回。'}</div>
          <p role="status">{{ connecting: '正在连接你的灵矽…', ready: '正在听你说', standby: '正在听你说', listening: '正在听你说', thinking: '正在了解当前棋局…', speaking: '灵矽正在回答' }[voiceState] || '安静观赛中，提问时才开启麦克风。'}</p>
          {voiceState === 'disconnected' ? <button className="voice-button" disabled={muted || !config?.voice_configured || !!closed || room.status === 'ended'} onClick={() => void ask()}><Microphone />{room.status === 'ended' ? '本局已结束' : muted ? '静音中' : config?.voice_configured ? '开始提问' : '灵矽未配置，可继续观看'}</button> : <button className="voice-button" onClick={() => voice.current?.disconnect()}>停止提问</button>}
          {voiceState === 'speaking' && <button className="text-button" onClick={() => voice.current?.stopPlayback()}>停止播放</button>}
          {error && <p role="alert">{error}</p>}
          {config?.diagnostics?.enabled && config.diagnostics.transcripts && <p className="voice-footnote">语音文本用于故障排查，保留{Math.ceil(config.diagnostics.retention_hours / 24)}天，不保存录音。你的对话不会展示给其他观众或选手。</p>}
          <button className="text-button" onClick={() => void leave()}>退出观赛 <ArrowClockwise /></button>
        </section>
        {!closed && <AudienceChat key={room.state.id} room={room} onRoom={accept} players={state.players} />}
      </aside>
    </div></main>
    {presentation.active?.kind === 'dice' && <DiceRollModal key={presentation.active.id} reducedMotion={reducedMotion} roll={presentation.active.roll} onClose={() => { if (presentation.active) present({ type: 'advance', id: presentation.active.id }); }} />}
  </div>;
}
