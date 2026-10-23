import { useCallback, useEffect, useLayoutEffect, useReducer, useRef, useState } from 'react';
import { AirplaneTilt, ArrowClockwise, ArrowRight, ArrowUpRight, Check, GearSix, House, Info, Microphone, Pause, Play, Plus, Stop, Question, Robot, SpeakerHigh, SpeakerSlash, Sparkle, Waveform, X } from '@phosphor-icons/react';
import { Celebration } from './components/Celebration';
import { HostNarration } from './components/HostNarration';
import { Narrator } from './lib/narration';
import { Board } from './components/Board';
import { DiceRollModal } from './components/DiceRollModal';
import { PropTray } from './components/PropTray';
import { api, APIError, palette, socketURL } from './lib/types';
import type { Config, Options, Piece, Player, Room } from './lib/types';
import { cuesFor, Sfx } from './lib/sfx';
import { VoiceClient } from './lib/voice';
import { initialPresentation, presentationReducer } from './lib/presentation';

const defaults: Options = { room_name: '周末家庭机场', difficulty: 'normal', waiting: true, names: ['小朋友', '爸爸', '灵矽'], board: 'cross', mode: 'quick', ai: true, demo: false, props: true };
const rules = [
  ['六点起飞', '掷到 6，可以把一架飞机从机场移到起飞位；还会再获得一次掷骰机会。'],
  ['同色跳跃', '落在自己的颜色格上会向前跳 4 格；到达星星飞行点，可以继续沿航线飞行。'],
  ['送回机场', '最终落点有其他玩家的飞机，就把它们送回机场。途经的飞机不会受影响。'],
  ['准确抵达', '进入终点需要恰好的点数，超过就选择其他飞机。快速模式先送达一架获胜。'],
  ['道具口袋', '趣味模式每人各有一张起飞卡、加速卡、幸运骰、炸弹和护盾。掷骰前最多用一张；起飞卡允许任意点数起飞，加速卡让在飞的飞机多走两格，幸运骰掷两次取较大值。炸弹送回对方公共航线上的一架飞机，护盾保护自己航线上的一架飞机，抵挡一次炸弹或撞机；机库、起飞位和终点航道不受影响。'],
];

function Avatar({ player, size = '' }: { player: Player; size?: string }) {
  return <span className={'avatar ' + player.color + ' ' + size}>{player.ai ? <Robot weight="duotone" /> : <AirplaneTilt weight="fill" />}</span>;
}

function Modal({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  const ref = useRef<HTMLDialogElement>(null);
  useEffect(() => { ref.current?.showModal(); }, []);
  return <dialog ref={ref} className="modal" aria-label={title} onCancel={onClose} onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
    <div className="modal-head"><h2>{title}</h2><button className="icon-button" aria-label="关闭" onClick={onClose}><X /></button></div>{children}
  </dialog>;
}

export default function App() {
  const [screen, setScreen] = useState<'home' | 'play'>(() => location.pathname === '/play' ? 'play' : 'home');
  const navigate = (next: 'home' | 'play') => {
    history.pushState({}, '', next === 'play' ? '/play' : '/');
    setScreen(next);
    window.scrollTo(0, 0);
  };
  useEffect(() => {
    const update = () => setScreen(location.pathname === '/play' ? 'play' : 'home');
    addEventListener('popstate', update);
    return () => removeEventListener('popstate', update);
  }, []);
  const [room, setRoom] = useState<Room>();
  const [config, setConfig] = useState<Config>();
  const [loading, setLoading] = useState(true);
  const [fatal, setFatal] = useState('');
  const [toast, setToast] = useState('');
  const [busy, setBusy] = useState(false);
  const [pendingAction, setPendingAction] = useState('');
  const [impact, setImpact] = useState<{ text: string; shield: boolean; crash: boolean }>();
  const [celebration, setCelebration] = useState<{ id: string; text: string; won: boolean }>();
  const [hostVoice, setHostVoice] = useState(() => localStorage.getItem('fly-host-voice') !== 'off');
  const narrator = useRef(new Narrator());
  const onAnnouncement = useCallback((text: string) => setSpeech({ text, who: 'linx' }), []);
  const [presentation, present] = useReducer(presentationReducer, initialPresentation);
  const [boardZoom, setBoardZoom] = useState(false);
  const [connected, setConnected] = useState(false);
  const [modal, setModal] = useState<'setup' | 'rules' | 'end' | 'props' | null>(null);
  const [setupNew, setSetupNew] = useState(false);
  const [options, setOptions] = useState<Options>(defaults);
  const [voiceState, setVoiceState] = useState('disconnected');
  const [autoVoice, setAutoVoice] = useState(() => localStorage.getItem('fly-auto-voice') !== 'off');
  const [autoVoiceError, setAutoVoiceError] = useState('');
  const autoVoiceAttempt = useRef('');
  const [speech, setSpeech] = useState({ text: '', who: '' });
  const [sound, setSound] = useState(() => localStorage.getItem('fly-sound') !== 'off');
  const voice = useRef<VoiceClient | undefined>(undefined);
  const roomRef = useRef<Room | undefined>(undefined);
  const loadingRef = useRef(false);
  const locked = useRef(false);
  const sfx = useRef(new Sfx());
  roomRef.current = room;

  const accept = useCallback((next: Room) => setRoom(old => old && old.id === next.id && old.seq > next.seq ? old : next), []);
  const load = useCallback(async () => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoading(true); setFatal('');
    try {
      const [configuration, session] = await Promise.all([api<Config>('/api/config'), api<{ room: Room | null }>('/api/session')]);
      setConfig(configuration);
      let next = session.room || await api<Room>('/api/rooms', defaults);
      // Migrate existing rooms too, so an old preference cannot restore wake gating.
      localStorage.removeItem('fly-voice-settings');
      if (next.voice_settings?.wake_enabled) {
        next = await api<Room>('/api/rooms/' + next.id + '/voice-settings', { ...next.voice_settings, wake_enabled: false });
      }
      accept(next);
    } catch (error) { setFatal(error instanceof Error ? error.message : '暂时无法连接游戏'); }
    finally { setLoading(false); loadingRef.current = false; }
  }, [accept]);
  useEffect(() => { void load(); return () => { voice.current?.disconnect(); }; }, [load]);
  useEffect(() => { if (!toast) return; const id = setTimeout(() => setToast(''), 4500); return () => clearTimeout(id); }, [toast]);
  useEffect(() => {
    if (!room?.id) return;
    let stopped = false, socket: WebSocket, timer: ReturnType<typeof setTimeout>;
    const connect = () => {
      socket = new WebSocket(socketURL('/ws/game?room_id=' + encodeURIComponent(room.id)));
      socket.onopen = () => setConnected(true);
      socket.onmessage = e => { try { const data = JSON.parse(e.data); if (data.type === 'state' && !stopped && roomRef.current?.id === room.id) accept(data.room); } catch {} };
      socket.onclose = () => { setConnected(false); if (!stopped) timer = setTimeout(connect, 1800); };
      socket.onerror = () => socket.close();
    };
    connect();
    return () => { stopped = true; clearTimeout(timer); socket.close(); setConnected(false); };
  }, [room?.id, accept]);

  const state = room?.state;
  const current = state?.players[state.current];
  useLayoutEffect(() => {
    if (state) present({ type: 'sync', game: state, status: room?.status || 'playing' });
  }, [state, room?.status]);
  useLayoutEffect(() => {
    const effect = presentation.effect;
    if (!effect) return;
    sfx.current.play(cuesFor(effect.events));
    const hit = [...effect.events].reverse().find(event => (event.kind === 'prop' && event.piece_id) || (event.kind === 'moved' && event.text.includes('回机场')));
    if (hit) setImpact({ text: hit.text, shield: hit.text.includes('护盾') && !hit.text.includes('送回'), crash: hit.kind === 'moved' });
    const home = [...effect.events].reverse().find(event => event.kind === 'won' || (event.kind === 'moved' && (event.text.includes('终点') || event.text.includes('抵达'))));
    if (home) setCelebration({ id: effect.id, text: home.text, won: home.kind === 'won' });
  }, [presentation.effect]);
  useEffect(() => {
    const beat = presentation.active;
    if (beat?.kind !== 'board') return;
    const timer = setTimeout(() => present({ type: 'advance', id: beat.id }), matchMedia('(prefers-reduced-motion: reduce)').matches ? 100 : beat.duration);
    return () => clearTimeout(timer);
  }, [presentation.active]);

  useEffect(() => {
    if (!impact) return;
    const timer = setTimeout(() => setImpact(undefined), 2600);
    return () => clearTimeout(timer);
  }, [impact]);
  useEffect(() => {
    if (!celebration) return;
    const timer = setTimeout(() => setCelebration(undefined), celebration.won ? 6000 : 4200);
    return () => clearTimeout(timer);
  }, [celebration]);
  useEffect(() => { setCelebration(undefined); setImpact(undefined); }, [state?.id]);
  const lifecycle = room?.status || 'playing';
  useEffect(() => {
    if (room?.status && room.status !== 'playing') {
      voice.current?.disconnect(); setImpact(undefined); setCelebration(undefined); narrator.current.stop();
    }
  }, [room?.id, room?.status]);
  useEffect(() => { sfx.current.enabled = sound; if (!sound) sfx.current.stop(); }, [sound]);
  useEffect(() => () => { sfx.current.dispose(); narrator.current.dispose(); }, []);
  const voiceBusy = room ? ['listening', 'thinking', 'speaking'].includes(room.voice) : false;
  const canAct = lifecycle === 'playing' && !!state && !!current && !current.ai && !busy && !voiceBusy && !presentation.active && !presentation.queue.length && state.phase !== 'finished';

  const command = useCallback(async (action: string, pieceID?: string, targetID?: string) => {
    const r = roomRef.current;
    if (!r || locked.current) return;
    void narrator.current.unlock();
    if (action === 'pause' || action === 'end') voice.current?.disconnect();
    locked.current = true; setBusy(true); setPendingAction(action);
    const body = { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action, piece_id: action === 'move' ? pieceID : action === 'prop' ? targetID : undefined, prop_id: action === 'prop' ? pieceID : undefined };
    const start = Date.now();
    try {
      let next: Room;
      try { next = await api<Room>('/api/rooms/' + r.id + '/commands', body); }
      catch (error) { if (error instanceof TypeError) next = await api<Room>('/api/rooms/' + r.id + '/commands', body); else throw error; }
      await new Promise(resolve => setTimeout(resolve, Math.max(0, 420 - (Date.now() - start))));
      if (roomRef.current?.id === r.id) {
        accept(next);
        if (action === 'start' || action === 'resume') sfx.current.play(['welcome']);
      }
    } catch (error) {
      setToast(error instanceof Error ? error.message : '这次操作没有完成');
      if (error instanceof APIError && (error.status === 409 || error.code === 'AI_TURN')) {
        const fresh = await api<{ room: Room | null }>('/api/session').catch(() => null);
        if (fresh?.room) accept(fresh.room);
      }
    } finally { locked.current = false; setBusy(false); setPendingAction(''); }
  }, [accept]);

  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.code === 'Space' && !modal && canAct && state?.phase === 'roll' && !(e.target instanceof HTMLInputElement) && !(e.target instanceof HTMLButtonElement)) {
        e.preventDefault(); void command('roll');
      }
    };
    window.addEventListener('keydown', key); return () => window.removeEventListener('keydown', key);
  }, [canAct, modal, state?.phase, command]);

  useEffect(() => {
    if (screen === 'home' && room?.status === 'playing' && room.state.phase !== 'finished' && !locked.current) void command('pause');
  }, [screen, room?.status, room?.state.id, command]);

  const onPiece = (piece: Piece, player: number) => {
    if (!state || !current) return;
    if (!canAct) { setToast(current.ai ? '轮到灵矽啦，稍等它走完。' : '请稍等这次操作完成。'); return; }
    if (state.current !== player) { setToast('现在轮到' + current.name + '，请选择自己的飞机。'); return; }
    if (state.phase === 'roll') { setToast('先掷骰子，再选择飞机。'); return; }
    if (!state.legal_moves.some(m => m.piece_id === piece.id)) {
      setToast(piece.progress === -1 ? '机库里的飞机需要掷到六点才能起飞。' : '这架飞机不能使用本次点数，试试高亮的飞机。'); return;
    }
    void command('move', piece.id);
  };

  const openSetup = (newRoom = false) => {
    if (busy || (lifecycle === 'playing' && (voiceBusy || current?.ai))) { setToast('请等这一回合结束后再调整。'); return; }
    setSetupNew(newRoom);
    setOptions({ room_name: newRoom ? '新的家庭机场' : room?.name || defaults.room_name, difficulty: state?.difficulty || 'normal', waiting: true, names: state?.players.map(p => p.name) || defaults.names, board: state?.board.id || 'cross', mode: state?.mode || 'quick', ai: state?.players.some(p => p.ai) || false, demo: false, props: state?.props_enabled ?? true });
    setModal('setup');
  };
  const restart = async (demo = false) => {
    if (!room || locked.current) return;
    locked.current = true; setBusy(true);
    try {
      void narrator.current.unlock();
      voice.current?.disconnect();
      let next = await api<Room>(setupNew ? '/api/rooms/new' : '/api/rooms/' + room.id + '/matches', { ...options, waiting: false, demo });
      accept(next); setModal(null); navigate('play');
      if (next.status === 'waiting') {
        next = await api<Room>('/api/rooms/' + next.id + '/commands', { command_id: crypto.randomUUID(), game_id: next.state.id, expected_version: next.state.version, action: 'start' });
        accept(next);
      }
      setSpeech({ text: '', who: '' }); sfx.current.play(['welcome']);
    }
    catch (error) { setToast(error instanceof Error ? error.message : '开局失败'); }
    finally { locked.current = false; setBusy(false); }
  };
  const connectVoice = useCallback(async () => {
    const r = roomRef.current;
    if (!r || r.status !== 'playing') return;
    autoVoiceAttempt.current = r.id + ':' + r.state.id;
    setAutoVoiceError('');
    voice.current?.disconnect();
    const client = new VoiceClient(r.id, setVoiceState, (text, who) => setSpeech({ text, who }), message => {
      setAutoVoiceError(message); setToast(message);
    });
    voice.current = client;
    try { await client.connect(); } catch (error) {
      if (voice.current !== client) return;
      const message = error instanceof Error ? error.message : '语音连接失败';
      setAutoVoiceError(message); setToast(message);
    }
  }, []);
  const changeAutoVoice = (enabled: boolean) => {
    void narrator.current.unlock();
    localStorage.setItem('fly-auto-voice', enabled ? 'on' : 'off');
    autoVoiceAttempt.current = '';
    setAutoVoice(enabled); setAutoVoiceError('');
    if (!enabled) voice.current?.disconnect();
  };
  useEffect(() => {
    if (screen !== 'play' || !autoVoice || !config?.voice_configured || room?.status !== 'playing' || room.state.phase === 'finished') {
      autoVoiceAttempt.current = '';
      if (screen !== 'play' || room?.state.phase === 'finished') voice.current?.disconnect();
      return;
    }
    const key = room.id + ':' + room.state.id;
    if (autoVoiceAttempt.current === key) return;
    autoVoiceAttempt.current = key;
    void connectVoice();
  }, [screen, autoVoice, config?.voice_configured, room?.id, room?.state.id, room?.status, room?.state.phase, connectVoice]);

  if (loading) return <div className="loading-page"><AirplaneTilt className="loading-plane" weight="fill" /><h1>正在准备家庭机场</h1><p>把棋盘铺好，快乐马上起飞。</p></div>;
  if (fatal || !room || !state || !current) return <div className="loading-page"><House size={48} /><h1>机场暂时还没连接上</h1><p>{fatal}</p><button className="primary-button" onClick={() => void load()}>重新连接 <ArrowClockwise /></button></div>;
  const winner = state.players.find(p => p.id === state.winner);
  const voiceLabels: Record<string, string> = { disconnected: '对讲麦克风已关闭', connecting: '正在连接', standby: '直接说话，我在听', ready: '直接说话，我在听', listening: '正在听你说', thinking: '正在理解', speaking: '灵矽正在说' };
  const effectiveVoice = voiceBusy ? room.voice : voiceState;
  return (
    <div className={'app-shell ' + (screen === 'play' ? 'play-shell' : 'home-shell')}>
      <header className="site-header">
        <button className="brand brand-button" onClick={async () => { voice.current?.disconnect(); if (lifecycle === 'playing' && state.phase !== 'finished') await command('pause'); navigate('home'); }} aria-label="返回首页"><span className="brand-mark"><AirplaneTilt size={26} weight="fill" /></span><span className="brand-name">一起飞<span>FlyTogether</span></span></button>
        <div className="header-actions">
          {screen === 'play' && <>
            {lifecycle === 'playing' && !winner && <button className="text-button" disabled={busy} onClick={() => void command('pause')}><Pause />暂停</button>}
            {lifecycle === 'paused' && <button className="text-button" disabled={busy} onClick={() => void command('resume')}><Play />继续游戏</button>}
            {lifecycle !== 'ended' && <button className="text-button" disabled={busy} onClick={() => setModal('end')}><Stop />结束游戏</button>}
            <button className="icon-button" aria-label={sound ? '关闭游戏音效' : '开启游戏音效'} onClick={() => { setSound(!sound); localStorage.setItem('fly-sound', sound ? 'off' : 'on'); }}>{sound ? <SpeakerHigh /> : <SpeakerSlash />}</button>
          </>}
          <button className="text-button" onClick={() => setModal('rules')}><Question size={18} />玩法</button>
        </div>
      </header>
      {screen === 'home' ? <main className="home-main">
        <section className="home-welcome">
          <p className="eyebrow"><span /> A LITTLE FAMILY ADVENTURE</p>
          <h1>叫上家人，<br />一起飞一局<span>。</span></h1>
          <p className="home-description">一张棋盘，一句“小灵同学”。<br />让每次起飞，都有人陪你欢呼。</p>
          <div className="home-actions"><button className="primary-button" onClick={() => openSetup(true)}><Plus size={21} />创建游戏<ArrowRight size={20} /></button>
            {lifecycle !== 'waiting' && <button className="outline-button" onClick={() => { navigate('play'); if (lifecycle === 'paused') void command('resume'); }}>继续当前游戏<ArrowRight size={18} /></button>}
          </div>
          <div className="home-features"><span>2—4 位飞行员</span><span>三种棋盘</span><span>语音游戏搭子</span></div>
        </section>
        <div className="home-flight-art" aria-hidden="true"><div className="home-orbit orbit-one" /><div className="home-orbit orbit-two" /><span className="home-cloud cloud-one" /><span className="home-cloud cloud-two" /><AirplaneTilt weight="fill" /><span className="home-flight-label">下一站，快乐到家。</span></div>
      </main> : <main className="play-main">
        <div className="play-layout">
          <section className="board-panel" aria-label="对局棋盘与玩家">
            <div className="board-turnbar"><span style={{ color: palette[current.color] }}><AirplaneTilt weight="fill" />{winner ? `${winner.name}获胜` : `${current.name}的回合`}</span>
              <strong>{state.demo ? '演示局面' : state.board.name}{state.phase === 'move' ? ` · ${state.dice} 点` : ''}</strong>
              <div className="play-turn-actions">{lifecycle === 'playing' && !winner && state.phase === 'roll' && <button className="primary-button roll-button" disabled={!canAct} onClick={() => void command('roll')}>{current.ai ? '灵矽的回合' : busy ? '骰子转呀转…' : '掷骰子'}</button>}
                {lifecycle === 'playing' && !winner && state.phase === 'move' && state.legal_moves.map(move => <button key={move.piece_id} className="prop-shortcut" disabled={!canAct} title={move.summary} onClick={() => void command('move', move.piece_id)}>{move.label}</button>)}
                {state.props_enabled && !winner && <button className="prop-shortcut" onClick={() => setModal('props')}>道具口袋 · {Object.values(current.props || {}).reduce((a, b) => a + b, 0)}</button>}
                {(lifecycle === 'ended' || winner) && <button className="prop-shortcut" onClick={() => openSetup(false)}>再来一局</button>}
              </div>
            </div>
            <div className={'board-viewport ' + (boardZoom ? 'is-zoomed' : '')}><div className="board-stage">{celebration && <Celebration key={celebration.id} text={celebration.text} won={celebration.won} />}{impact && <div className={"board-impact " + (impact.shield ? "shield-impact" : "bomb-impact")} role="status"><Sparkle weight="fill" /><strong>{impact.shield ? "护盾启动" : impact.crash ? "被撞回机场" : "炸弹出击"}</strong><span>{impact.text}</span></div>}{lifecycle !== 'playing' && <div className="game-status-overlay"><strong>{lifecycle === 'waiting' ? '飞行员准备好了吗？' : lifecycle === 'paused' ? '先休息一下' : '这一趟飞行已结束'}</strong><p>{lifecycle === 'waiting' ? '确认玩家和难度后，点击开始游戏。' : lifecycle === 'paused' ? '棋盘、道具和骰子已保留，随时继续。' : '保留本局记录，重新准备下一次出发。'}</p><button className="primary-button" disabled={busy} onClick={() => lifecycle === 'ended' ? openSetup(false) : void command(lifecycle === 'waiting' ? 'start' : 'resume')}><Play weight="fill" />{lifecycle === 'waiting' ? '开始游戏' : lifecycle === 'paused' ? '继续游戏' : '重新准备'}</button></div>}<Board game={presentation.game || state} onPiece={onPiece} disabled={!canAct} onRoll={() => void command('roll')} rolling={busy && pendingAction === 'roll'} /></div></div>
            <div className="crew-list" style={{ '--seats': state.players.length } as React.CSSProperties}>
              {state.players.map((player, i) => <div key={player.id} className={'crew-seat ' + (state.current === i && state.phase !== 'finished' ? 'current' : '')} style={{ '--player-color': palette[player.color] } as React.CSSProperties}>
                <Avatar player={player} /><div className="crew-detail"><strong>{player.name}{player.ai && <span className="ai-tag">AI</span>}</strong><span>{player.pieces.filter(p => p.progress === state.board.finish).length}/{state.mode === 'quick' ? '1' : player.pieces.length} 架抵达</span></div>{state.current === i && state.phase !== 'finished' && <span className="seat-turn">本回合</span>}
              </div>)}
            </div>
          </section>
          <aside className="side-column" aria-label="语音助手">
            <section className="voice-panel">
              <div className="voice-heading"><span className="voice-icon"><Waveform size={24} /></span><div><h2>灵矽，陪你一起飞</h2><p>你的家庭游戏搭子</p></div><span className="voice-spark"><Sparkle size={22} weight="duotone" /></span></div>
              <label className="auto-voice-setting"><input type="checkbox" checked={hostVoice} onChange={e => { setHostVoice(e.target.checked); localStorage.setItem('fly-host-voice', e.target.checked ? 'on' : 'off'); void narrator.current.unlock(); }} />关闭麦克风时也播报回合</label>
              <HostNarration roomID={room.id} game={presentation.game || state} enabled={screen === 'play' && lifecycle === 'playing' && hostVoice && sound && !!config?.voice_configured && voiceState === 'disconnected' && (!autoVoice || !!autoVoiceError) && !presentation.active && !presentation.queue.length} narrator={narrator.current} onText={onAnnouncement} />
              <label className="auto-voice-setting"><input type="checkbox" checked={autoVoice} onChange={e => changeAutoVoice(e.target.checked)} />自动连接语音，直接说话</label>
              <div className="voice-bubble" aria-live="polite">{speech.text ? <><span className="speech-label">{speech.who === 'you' ? '你刚刚说' : '灵矽'}</span>{speech.text}</> : <><span className="bubble-quote">“</span>直接说「掷骰子」或<br />「让小熊号起飞」，我来帮你。</>}</div>
              <div className="voice-status"><span className={'status-dot ' + (voiceState === 'ready' ? 'online' : '')} />{config?.voice_configured ? voiceLabels[effectiveVoice] || '等待连接' : '语音待配置 · 点击玩法已就绪'}</div>
              {voiceState === 'disconnected' || voiceState === 'connecting' ? <button className="voice-button" disabled={lifecycle !== 'playing' || !!winner || !config?.voice_configured || voiceState === 'connecting'} onClick={() => { changeAutoVoice(true); void connectVoice(); }}><Microphone size={20} />{lifecycle === 'paused' ? '继续游戏后自动连接语音' : lifecycle === 'ended' ? '新开一局后自动连接语音' : lifecycle !== 'playing' ? '开始游戏后自动连接语音' : voiceState === 'connecting' ? '正在准备麦克风…' : config?.voice_configured ? (autoVoiceError ? '点击启用麦克风并重试' : '开启自动语音') : '语音暂未开启'}</button> :
                <button className={'voice-button ' + (effectiveVoice === 'listening' ? 'listening' : '')} onClick={() => changeAutoVoice(false)}><Microphone size={21} />关闭对讲麦克风</button>}
              {autoVoiceError && <p className="voice-connection-error" role="alert">{autoVoiceError}</p>}
              {effectiveVoice === 'speaking' && <button className="stop-speech" onClick={() => voice.current?.stopPlayback()}>停止播报</button>}
              <div className="voice-examples"><strong>试着跟我说</strong><span>“掷骰子”</span><span>“使用起飞卡”</span><span>“让小熊号起飞”</span><span>“现在轮到谁”</span></div><p className="voice-footnote">{!config?.voice_configured ? '语音连接完成后，就能开口操作和询问规则。' : `${autoVoice ? '开始游戏后自动连接' : '开启语音后'}，直接说话即可。首次需允许麦克风；开启期间音频会发送给灵矽识别，可随时关闭。`}</p>
            </section>
          </aside>
        </div>
      </main>}
      {presentation.active?.kind === 'dice' && <DiceRollModal key={presentation.active.id} roll={presentation.active.roll} onClose={() => { if (presentation.active) present({ type: 'advance', id: presentation.active.id }); }} />}
      {toast && <div className="toast" role="status"><Info size={19} /><span>{toast}</span><button aria-label="关闭提示" onClick={() => setToast('')}><X size={16} /></button></div>}
      {modal === 'props' && <Modal title={`${current.name}的道具口袋`} onClose={() => setModal(null)}><PropTray key={state.id + ':' + state.version} game={state} status={lifecycle} disabled={!canAct} onUse={(id, target) => { setModal(null); void command('prop', id, target); }} /></Modal>}
      {modal === 'rules' && <Modal title="一起飞，怎么玩？" onClose={() => setModal(null)}><p className="modal-description">这是一起飞的家庭规则。快速模式先送达一架飞机获胜；完整模式需要送达全部飞机。</p><div className="rules-list">{rules.map(([title, text], i) => <div key={title}><span>{String(i + 1).padStart(2, '0')}</span><section><h3>{title}</h3><p>{text}</p></section></div>)}</div><p className="rule-note">机库、起飞位和自己的终点航道不参与撞机。相同位置可以叠放；不使用三连六惩罚或终点反弹。</p><button className="primary-button" onClick={() => setModal(null)}>懂啦，一起出发 <Check /></button></Modal>}
      {modal === 'end' && <Modal title="结束这一局？" onClose={() => setModal(null)}><p className="modal-description">结束后停止当前对局并保留棋盘记录，可以重新准备下一局。</p><button className="primary-button" disabled={busy} onClick={() => { setModal(null); void command('end'); }}>结束游戏 <Stop weight="fill" /></button></Modal>}
      {modal === 'setup' && <Modal title={setupNew ? '创建家庭房间' : '准备下一次出发'} onClose={() => setModal(null)}>
        <form onSubmit={e => { e.preventDefault(); void restart(false); }}>
          <p className="modal-description">选好玩家、路线和难度，创建后直接进入大棋盘开始游戏。</p>
          <label className="field-label" htmlFor="room-name">房间名称</label><input id="room-name" className="room-name-input" value={options.room_name} maxLength={20} required onChange={e => setOptions(o => ({...o, room_name: e.target.value}))} />
          <label className="field-label">AI 难度</label><div className="difficulty-options">{([{id:'easy',name:'轻松',desc:'随机选子，不使用道具'},{id:'normal',name:'普通',desc:'优先好走法，使用辅助道具'},{id:'hard',name:'挑战',desc:'规划走法，主动攻击与防守'}] as const).map(level => <button key={level.id} type="button" aria-pressed={options.difficulty === level.id} className={options.difficulty === level.id ? 'selected' : ''} onClick={() => setOptions(o => ({...o,difficulty:level.id}))}><strong>{level.name}</strong><span>{level.desc}</span></button>)}</div><p className="wake-help">难度只影响 AI 对手，不改变随机骰子和真人规则；未邀请 AI 时不会生效。</p>
          <label className="field-label">棋盘路线</label><div className="board-options">{(config?.boards || []).map(board => <button type="button" key={board.id} className={options.board === board.id ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, board: board.id }))}><strong>{board.name}</strong><span>{board.ring_length} 个航线格</span><small>{board.home_length} 格终点跑道</small></button>)}</div>
          <label className="field-label">飞行员席位</label><div className="segmented">{[2, 3, 4].map(count => <button type="button" key={count} className={options.names.length === count ? 'selected' : ''} onClick={() => setOptions(o => {
            const used = new Set<string>();
            const names = Array.from({ length: count }, (_, i) => {
              let name = o.ai && i === count - 1 ? '灵矽' : (o.names[i] && o.names[i] !== '灵矽' ? o.names[i] : ['小朋友', '爸爸', '妈妈', '家人'][i]);
              if (used.has(name)) name = ['小朋友', '爸爸', '妈妈', '家人', '飞行员'].find(candidate => !used.has(candidate))!;
              used.add(name); return name;
            });
            return { ...o, names };
          })}>{count} 位</button>)}</div>
          <div className="name-fields">{options.names.map((name, i) => <label key={i}><span className="name-color" style={{ background: palette[['coral', 'gold', 'blue', 'jade'][i]] }} /><span className="sr-only">第 {i + 1} 位飞行员昵称</span><input maxLength={10} required value={name} onChange={e => setOptions(o => ({ ...o, names: o.names.map((v, n) => n === i ? e.target.value : v) }))} />{options.ai && i === options.names.length - 1 && <Robot size={20} />}</label>)}</div>
          <label className="toggle-row"><span><strong>邀请灵矽加入</strong><small>最后一个席位交给 AI 飞行员</small></span><input type="checkbox" checked={options.ai} onChange={e => setOptions(o => ({ ...o, ai: e.target.checked, names: o.names.map((name, i) => i === o.names.length - 1 ? (e.target.checked ? '灵矽' : ['小朋友', '爸爸', '妈妈', '家人'][i]) : name) }))} /></label>
          <label className="toggle-row"><span><strong>趣味道具模式</strong><small>每人五张道具：起飞、加速、幸运、炸弹、护盾</small></span><input type="checkbox" checked={options.props} onChange={e => setOptions(o => ({ ...o, props: e.target.checked }))} /></label>
          <label className="field-label">这一局的节奏</label><div className="mode-options"><button type="button" className={options.mode === 'quick' ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, mode: 'quick' }))}><Sparkle /><strong>轻快一局</strong><span>每人 2 架 · 先到 1 架获胜</span></button><button type="button" className={options.mode === 'classic' ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, mode: 'classic' }))}><AirplaneTilt /><strong>完整飞行</strong><span>每人 4 架 · 全部抵达获胜</span></button></div>
          <button className="primary-button" disabled={busy} type="submit">{busy ? '准备机场中…' : (setupNew ? '创建并开始游戏' : '开始新的一局')}<ArrowRight /></button>
          <button type="button" disabled={busy} className="demo-link" onClick={() => void restart(true)}>试试预设演示局面 <ArrowUpRight size={14} /></button>
        </form>
      </Modal>}
    </div>
  );
}
