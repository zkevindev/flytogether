import { useExperience } from './lib/experience';
import { focusGame, useDialog } from './lib/dialog';
import { FamilyPanel, ProfilePicker, avatarIcon } from './components/FamilyPanel';
import { Suspense, lazy, useCallback, useEffect, useLayoutEffect, useReducer, useRef, useState } from 'react';
import { AirplaneTilt, ArrowClockwise, ArrowRight, ArrowUpRight, Check, GearSix, House, Info, Microphone, Pause, Play, Plus, Stop, Question, Robot, SpeakerHigh, SpeakerSlash, Sparkle, Waveform, X } from '@phosphor-icons/react';
import { Tutorial, tutorialPreference } from './components/Tutorial';
import { MovePreview } from './components/MovePreview';
import { Celebration } from './components/Celebration';
import { HostNarration } from './components/HostNarration';
import { loadCompanion } from './lib/companion';
import { Narrator } from './lib/narration';
import { BoardLoader } from './components/BoardLoader';
import { DiceRollModal } from './components/DiceRollModal';
import { PropTray } from './components/PropTray';
import { api, APIError, palette, socketURL } from './lib/types';
import type { CompanionPreferences, Config, Family, Game, Options, Piece, Player, Room } from './lib/types';
import { cuesFor, Sfx } from './lib/sfx';
import { VoiceClient } from './lib/voice';
import { initialPresentation, presentationReducer } from './lib/presentation';

const ExperienceSettings = lazy(() => import('./components/ExperienceSettings').then(module => ({default:module.ExperienceSettings})));

const defaults: Options = { room_name: '周末家庭机场', difficulty: 'normal', waiting: true, names: ['小朋友', '爸爸', '灵矽机器人'], board: 'cross', mode: 'quick', ai: true, demo: false, props: true };
const rules = (game: Game) => [
  ['起飞与加掷', `${game.preset === 'gentle' ? '掷到 5 或 6' : '掷到 6'}可以把一架飞机从机场移到起飞位；只有掷到 6 才再获得一次掷骰机会。`],
  ['同色跳跃', '落在自己的颜色格上会向前跳 4 格；到达星星飞行点，可以继续沿航线飞行。'],
  ['送回机场', '最终落点有其他玩家的飞机，就把它们送回机场。途经的飞机不会受影响。'],
  ['准确抵达', '进入终点需要恰好的点数，超过就选择其他飞机。快速模式先送达一架获胜。'],
  ['道具口袋', !game.props_enabled ? '本局不使用道具，所有玩家遵循相同的起飞、移动和胜利条件。' : `${game.preset === 'gentle' ? '本局每人起飞卡2张、加速卡1张、幸运骰1张、护盾2张，不提供炸弹。' : '本局五种道具各1张。'}掷骰前最多用一张；起飞卡允许任意点数起飞，加速卡让在飞的飞机多走两格，幸运骰使用后立即掷两次取较大值。炸弹送回对方公共航线上的一架飞机，护盾保护自己航线上的一架飞机，抵挡一次炸弹或撞机；机库、起飞位和终点航道不受影响。`],
];

function Avatar({ player, size = '' }: { player: Player; size?: string }) {
  return <span className={'avatar ' + player.color + ' ' + size}>{player.ai ? <Robot weight="duotone" /> : player.avatar ? <span aria-hidden="true">{avatarIcon(player.avatar)}</span> : <AirplaneTilt weight="fill" />}</span>;
}

function Modal({ title, children, onClose }: { title: string; children: React.ReactNode; onClose: () => void }) {
  const ref = useDialog();
  return <dialog ref={ref} className="modal" aria-label={title} onCancel={onClose} onClick={e => { if (e.target === e.currentTarget) onClose(); }}>
    <div className="modal-head"><h2>{title}</h2><button className="icon-button" aria-label="关闭" onClick={onClose}><X /></button></div>{children}
  </dialog>;
}

export default function App() {
  const { preferences: experience, change: changeExperience, reducedMotion, systemReduced } = useExperience();
  const focusPreview = useRef(false);
  const hadPreview = useRef(false);
  const keyboardInput = useRef(false);
  const [keyboardMode, setKeyboardMode] = useState(false);
  const [invite, setInvite] = useState(() => new URLSearchParams(location.hash.slice(1)).get('join') || '');
  const [joinName, setJoinName] = useState('');
  const [joinProfile, setJoinProfile] = useState('');
  const [family,setFamily] = useState<Family>({profiles:[],matches:[],total:0});
  const [joinError, setJoinError] = useState('');
  const [resultDismissed, setResultDismissed] = useState('');
  const [screen, setScreen] = useState<'home' | 'play'>(() => location.pathname === '/play' ? 'play' : 'home');
  const navigate = useCallback((next: 'home' | 'play') => {
    history.pushState({}, '', next === 'play' ? '/play' : '/');
    setScreen(next);
    window.scrollTo(0, 0);
  }, []);
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
  const [tutorialSeen, setTutorialSeen] = useState(() => !!localStorage.getItem(tutorialPreference));
  const [tutorialOpen, setTutorialOpen] = useState(false);
  const [previewSelection, setPreviewSelection] = useState<{ gameID: string; version: number; pieceID: string }>();
  const [impact, setImpact] = useState<{ text: string; shield: boolean; crash: boolean }>();
  const [celebration, setCelebration] = useState<{ id: string; text: string; won: boolean }>();
  const [companion, setCompanion] = useState(loadCompanion);
  const companionRef = useRef(companion); companionRef.current = companion;
  const [hostVoice, setHostVoice] = useState(() => localStorage.getItem('fly-host-voice') !== 'off');
  const narrator = useRef(new Narrator());
  const onAnnouncement = useCallback((text: string) => setSpeech({ text, who: 'linx' }), []);
  const [presentation, present] = useReducer(presentationReducer, initialPresentation);
  const [boardZoom, setBoardZoom] = useState(false);
  const [connected, setConnected] = useState(false);
  const [modal, setModal] = useState<'setup' | 'rules' | 'end' | 'props' | 'invite' | 'discard' | 'audio' | 'companion' | 'family' | 'experience' | null>(null);
  const [setupNew, setSetupNew] = useState(false);
  const [options, setOptions] = useState<Options>(defaults);
  const [voiceState, setVoiceState] = useState('disconnected');
  const [autoVoice, setAutoVoice] = useState(() => localStorage.getItem('fly-auto-voice') !== 'off');
  const [autoVoiceError, setAutoVoiceError] = useState('');
  const autoVoiceAttempt = useRef('');
  const [speech, setSpeech] = useState({ text: '', who: '' });
  const [masterMuted, setMasterMuted] = useState(() => localStorage.getItem('fly-master-muted') === 'on');
  const [sound, setSound] = useState(() => localStorage.getItem('fly-sound') !== 'off');
  const voiceAllowedRef = useRef(false);
  const voice = useRef<VoiceClient | undefined>(undefined);
  const roomRef = useRef<Room | undefined>(undefined);
  const loadingRef = useRef(false);
  const locked = useRef(false);
  const sfx = useRef(new Sfx());
  roomRef.current = room;

  const obsoleteInstances = useRef(new Set<string>());
  const accept = useCallback((next: Room) => setRoom(old => {
    if (old?.id === next.id) {
      if (old.instance_id && next.instance_id && old.instance_id !== next.instance_id) {
        if (obsoleteInstances.current.has(next.instance_id)) return old;
        obsoleteInstances.current.add(old.instance_id);
      } else if (old.seq > next.seq) return old;
    }
    return next;
  }), []);
  const load = useCallback(async () => {
    if (loadingRef.current) return;
    loadingRef.current = true;
    setLoading(true); setFatal('');
    try {
      const [configuration, session] = await Promise.all([api<Config>('/api/config'), api<{ room: Room | null }>('/api/session')]);
      setConfig(configuration);
      try { setFamily(await api<Family>('/api/family')); } catch { /* Family availability does not block the current game. */ }
      if (invite) { if (session.room) accept(session.room); return; }
      let next = session.room || await api<Room>('/api/rooms', defaults);
      // Migrate existing rooms too, so an old preference cannot restore wake gating.
      localStorage.removeItem('fly-voice-settings');
      if (next.voice_settings?.wake_enabled) {
        next = await api<Room>('/api/rooms/' + next.id + '/voice-settings', { ...next.voice_settings, wake_enabled: false });
      }
      accept(next);
    } catch (error) { setFatal(error instanceof Error ? error.message : '暂时无法连接游戏'); }
    finally { setLoading(false); loadingRef.current = false; }
  }, [accept, invite]);
  useEffect(() => { void load(); return () => { voice.current?.disconnect(); }; }, [load]);
  useEffect(() => { if (!toast) return; const id = setTimeout(() => setToast(''), 4500); return () => clearTimeout(id); }, [toast]);
  useEffect(() => {
    if (!room?.id) return;
    let stopped = false, socket: WebSocket, timer: ReturnType<typeof setTimeout>;
    const connect = () => {
      if (stopped || !navigator.onLine) { setConnected(false); return; }
      socket = new WebSocket(socketURL('/ws/game?room_id=' + encodeURIComponent(room.id)));
      socket.onopen = () => setConnected(false);
      socket.onmessage = e => { try { const data = JSON.parse(e.data); if (data.type === 'state' && !stopped && roomRef.current?.id === room.id) { accept(data.room); setConnected(true); } } catch {} };
      socket.onclose = () => { setConnected(false); if (!stopped && navigator.onLine) timer = setTimeout(connect, 1800); };
      socket.onerror = () => socket.close();
    };
    const offline = () => { clearTimeout(timer); setConnected(false); socket?.close(); voice.current?.disconnect(); };
    const online = () => { clearTimeout(timer); connect(); };
    addEventListener('offline', offline); addEventListener('online', online);
    connect();
    return () => { stopped = true; removeEventListener('offline', offline); removeEventListener('online', online); clearTimeout(timer); socket?.close(); setConnected(false); };
  }, [room?.id, accept]);

  const state = room?.state;
  const current = state?.players[state.current];
  const network = !!room?.network;
  const host = !network || !!room?.me?.host;
  const myTurn = !network || room?.me?.player_id === current?.id;
  const allReady = !!room?.network?.seats.every(s => s.occupied && s.ready && s.online);
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
    const timer = setTimeout(() => present({ type: 'advance', id: beat.id }), reducedMotion ? 80 : beat.duration);
    return () => clearTimeout(timer);
  }, [presentation.active, reducedMotion]);

  useEffect(() => {
    if (!impact) return;
    const timer = setTimeout(() => setImpact(undefined), 2600);
    return () => clearTimeout(timer);
  }, [impact]);
  useEffect(() => {
    if (!celebration) return;
    const timer = setTimeout(() => setCelebration(undefined), reducedMotion && celebration.won ? 1200 : celebration.won ? 6000 : 4200);
    return () => clearTimeout(timer);
  }, [celebration, reducedMotion]);
  useEffect(() => { setCelebration(undefined); setImpact(undefined); }, [state?.id]);
  const lifecycle = room?.status || 'playing';
  const voiceAllowed = !masterMuted && myTurn && connected && screen === 'play' && lifecycle === 'playing' && state?.phase !== 'finished' && tutorialSeen && !tutorialOpen;
  voiceAllowedRef.current = voiceAllowed;
  useEffect(() => {
    if (room?.status === 'ended' && room.network) navigate('home');
    if (room?.status && room.status !== 'playing') {
      voice.current?.disconnect(); setImpact(undefined); setCelebration(undefined); narrator.current.stop();
    }
  }, [room?.id, room?.status, navigate]);
  useEffect(() => { sfx.current.enabled = sound && !masterMuted; if (!sound || masterMuted) sfx.current.stop(); }, [sound, masterMuted]);
  useEffect(() => () => { sfx.current.dispose(); narrator.current.dispose(); }, []);
  const voiceBusy = room ? ['listening', 'thinking', 'speaking'].includes(room.voice) : false;
  const canAct = myTurn && connected && !tutorialOpen && lifecycle === 'playing' && !!state && !!current && !current.ai && !busy && !voiceBusy && !presentation.active && !presentation.queue.length && state.phase !== 'finished';

  const coach = room?.coach && room.coach.game_id === state?.id && room.coach.version === state.version && room.coach.player_id === current?.id ? room.coach : undefined;
  const canPreview = myTurn && connected && !tutorialOpen && lifecycle === 'playing' && !current?.ai && state?.phase === 'move' && !presentation.active && !presentation.queue.length;
  const manualPreview = previewSelection?.gameID === state?.id && previewSelection?.version === state?.version
    ? state?.legal_moves.find(move => move.piece_id === previewSelection?.pieceID) : undefined;
  const previewCoach = !manualPreview ? coach : undefined;
  const preview = canPreview ? manualPreview || previewCoach?.move : undefined;
  useEffect(() => { if (room?.coach) setPreviewSelection(undefined); }, [room?.coach?.id]);
  useEffect(() => { setPreviewSelection(undefined); }, [state?.id, state?.version, lifecycle, screen]);
  useEffect(() => {
    if (screen === 'play' && lifecycle === 'playing' && state?.phase !== 'finished' && !tutorialSeen && !modal && !presentation.active && !presentation.queue.length) setTutorialOpen(true);
  }, [screen, lifecycle, state?.phase, tutorialSeen, modal, presentation.active, presentation.queue.length]);
  useEffect(() => {
    if (tutorialOpen) { setPreviewSelection(undefined); narrator.current.stop(); setSpeech({ text: '', who: '' }); }
  }, [tutorialOpen]);
  const closeTutorial = (completed: boolean) => {
    localStorage.setItem(tutorialPreference, completed ? 'completed' : 'skipped');
    setTutorialSeen(true); setTutorialOpen(false);
  };
  const selectMove = (pieceID: string) => {
    if (!canAct || state?.phase !== 'move' || !state.legal_moves.some(move => move.piece_id === pieceID)) return;
    focusPreview.current = keyboardInput.current;
    setPreviewSelection({ gameID: state.id, version: state.version, pieceID });
    if (room?.coach) void command('coach', 'cancel');
  };

  const command = useCallback(async (action: string, pieceID?: string, targetID?: string) => {
    const r = roomRef.current;
    if (!r || locked.current) return;
    void narrator.current.unlock();
    if (action === 'pause' || action === 'end') voice.current?.disconnect();
    locked.current = true; setBusy(true); setPendingAction(action);
    const body = { command_id: crypto.randomUUID(), game_id: r.state.id, expected_version: r.state.version, action, piece_id: action === 'move' ? pieceID : action === 'prop' ? targetID : undefined, prop_id: action === 'prop' ? pieceID : undefined, choice: action === 'coach' ? pieceID : undefined, coach_id: action === 'confirm_coach' ? pieceID : undefined };
    const start = Date.now();
    try {
      let next: Room;
      try { next = await api<Room>('/api/rooms/' + r.id + '/commands', body); }
      catch (error) { if (error instanceof TypeError) next = await api<Room>('/api/rooms/' + r.id + '/commands', body); else throw error; }
      await new Promise(resolve => setTimeout(resolve, Math.max(0, 420 - (Date.now() - start))));
      if (roomRef.current?.id === r.id) {
        accept(next);
        if (action === 'end') navigate('home');
        if (action === 'start' || action === 'resume') sfx.current.play(['welcome']);
      }
    } catch (error) {
      setToast(error instanceof Error ? error.message : '这次操作没有完成');
      if (error instanceof APIError && (error.status === 409 || error.code === 'AI_TURN')) {
        const fresh = await api<{ room: Room | null }>('/api/session').catch(() => null);
        if (fresh?.room) accept(fresh.room);
      }
    } finally { locked.current = false; setBusy(false); setPendingAction(''); }
  }, [accept, navigate]);

  useEffect(() => {
    if (preview && focusPreview.current) { document.querySelector<HTMLElement>('.move-preview')?.focus({ preventScroll: true }); focusPreview.current = false; }
    if (hadPreview.current && !preview && document.activeElement === document.body) focusGame();
    hadPreview.current = !!preview;
  }, [preview?.piece_id]);
  useEffect(() => {
    const keyboard = (e: KeyboardEvent) => { keyboardInput.current = true; setKeyboardMode(true); if(e.repeat && (e.key==='Enter'||e.code==='Space') && e.target instanceof Element && !e.target.closest('input,textarea,select,[contenteditable]')) e.preventDefault(); };
    const pointer = () => { keyboardInput.current = false; setKeyboardMode(false); };
    addEventListener('keydown', keyboard, true); addEventListener('pointerdown', pointer, true);
    return () => { removeEventListener('keydown', keyboard, true); removeEventListener('pointerdown', pointer, true); };
  }, []);
  useEffect(() => {
    const frame = requestAnimationFrame(() => { if (!document.querySelector('dialog[open]')) document.querySelector<HTMLElement>('main')?.focus({ preventScroll: true }); });
    return () => cancelAnimationFrame(frame);
  }, [screen, loading]);
  useEffect(() => {
    const key = (e: KeyboardEvent) => {
      if (e.repeat || e.altKey || e.ctrlKey || e.metaKey || screen !== 'play' || document.querySelector('dialog[open]')) return;
      const target = e.target instanceof Element ? e.target : null;
      if (target?.closest('input,textarea,select,[contenteditable]')) return;
      if (e.key === 'Escape' && !modal && (previewSelection || coach) && canAct) { e.preventDefault(); setPreviewSelection(undefined); if (coach) void command('coach', 'cancel'); focusGame(); return; }
      if (target?.closest('button,a,summary,[role="button"]') || modal || !canAct) return;
      if (e.code === 'Space' && state?.phase === 'roll') { e.preventDefault(); void command('roll'); }
      if (/^[1-4]$/.test(e.key) && state?.phase === 'move') { const piece=current?.pieces.find(p=>p.number===Number(e.key)); if (piece) { e.preventDefault(); selectMove(piece.id); } }
      if (e.key.toLowerCase() === 'p' && state?.props_enabled) { e.preventDefault(); setModal('props'); }
      if (e.key === 'Enter' && preview) { e.preventDefault(); if (previewCoach) void command('confirm_coach', previewCoach.id); else void command('move', preview.piece_id); }
    };
    window.addEventListener('keydown', key); return () => window.removeEventListener('keydown', key);
  }, [canAct, modal, previewSelection, coach, preview, previewCoach, state, current, screen, command]);

  useEffect(() => {
    if (!room?.network && screen === 'home' && room?.status === 'playing' && room.state.phase !== 'finished' && !locked.current) void command('pause');
  }, [screen, room?.status, room?.state.id, command]);

  const onPiece = (piece: Piece, player: number) => {
    if (!state || !current) return;
    if (!canAct) { setToast(current.ai ? '机器人正在自动行动，稍等它走完。' : '请稍等这次操作完成。'); return; }
    if (state.current !== player) { setToast('现在轮到' + current.name + '，请选择自己的飞机。'); return; }
    if (state.phase === 'roll') { setToast('先掷骰子，再选择飞机。'); return; }
    if (!state.legal_moves.some(m => m.piece_id === piece.id)) {
      setToast(piece.progress === state.board.finish ? '这架飞机已经抵达终点，选其他飞机继续。' : piece.progress === -1 ? `机库里的飞机需要掷到${state.preset === 'gentle' ? '五点或六点' : '六点'}${state.props_enabled && current.props.launch > 0 ? '或使用起飞卡' : ''}才能起飞。` : '这架飞机不能使用本次点数，试试高亮的飞机。'); return;
    }
    selectMove(piece.id);
  };

  const openSetup = (newRoom = false) => {
    if (busy || (lifecycle === 'playing' && (voiceBusy || current?.ai))) { setToast('请等这一回合结束后再调整。'); return; }
    setSetupNew(newRoom);
    setOptions({ profile_ids: room?.profile_ids?.map(id=>family.profiles.some(p=>p.id===id)?id:''), avatars:state?.players.map(p=>p.avatar||''), preset: state?.preset || 'custom', room_name: newRoom ? '新的家庭机场' : room?.name || defaults.room_name, difficulty: state?.difficulty || 'normal', waiting: true, names: state?.players.map(p => p.name) || defaults.names, board: state?.board.id || 'cross', mode: state?.mode || 'quick', ai: state?.players.some(p => p.ai) || false, demo: false, props: state?.props_enabled ?? true, network: newRoom ? false : !!room?.network });
    setModal('setup');
  };
  const restart = async (demo = false) => {
    if (!room || locked.current) return;
    locked.current = true; setBusy(true);
    try {
      void narrator.current.unlock();
      voice.current?.disconnect();
      let next = await api<Room>(setupNew ? '/api/rooms/new' : '/api/rooms/' + room.id + '/matches', { ...options, profile_ids:options.names.map((_,i)=>options.network&&i>0||options.ai&&i===options.names.length-1?'':options.profile_ids?.[i]||''), avatars:options.names.map((_,i)=>options.network&&i>0||options.ai&&i===options.names.length-1?'':options.avatars?.[i]||''), waiting: false, demo });
      accept(next); setModal(null); navigate('play');
      if (next.status === 'waiting' && !next.network) {
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
    if (!r || r.status !== 'playing' || !voiceAllowedRef.current) return;
    autoVoiceAttempt.current = r.id + ':' + r.state.id;
    setAutoVoiceError('');
    voice.current?.disconnect();
    const reportVoiceFailure=(message:string)=>{
      if(!config?.diagnostics?.enabled)return;
      const code=/权限|Permission|NotAllowed/i.test(message)?'microphone_denied':/声音|AudioContext/.test(message)?'audio_unlock':/过长|积压/.test(message)?'playback_overflow':/解码/.test(message)?'audio_decode':/断开|中断/.test(message)?'voice_disconnected':'voice_failed';
      void api('/api/rooms/'+r.id+'/diagnostics',{game_id:r.state.id,code}).catch(()=>{});
    };
    const client = new VoiceClient(r.id, setVoiceState, (text, who) => { if(who === 'you') { keyboardInput.current = false; setKeyboardMode(false); } setSpeech({ text, who }); }, message => {
      setAutoVoiceError(message); setToast(message); reportVoiceFailure(message);
    }, companionRef.current, next => { if (roomRef.current?.id === next.id) accept(next); });
    voice.current = client;
    try { await client.connect(); } catch (error) {
      if (voice.current !== client) return;
      const message = error instanceof Error ? error.message : '语音连接失败';
      setAutoVoiceError(message); setToast(message); reportVoiceFailure(message);
    }
  }, [accept]);
  const changeCompanion = (next: CompanionPreferences) => {
    localStorage.setItem('fly-companion', JSON.stringify(next));
    narrator.current.stop(); voice.current?.setCompanion(next); setCompanion(next);
  };
  const changeMasterMute = (muted: boolean) => {
    localStorage.setItem('fly-master-muted', muted ? 'on' : 'off');
    if (muted) { voiceAllowedRef.current = false; voice.current?.disconnect(); narrator.current.stop(); sfx.current.enabled = false; sfx.current.stop(); }
    else void narrator.current.unlock();
    setMasterMuted(muted);
  };
  const changeHostVoice = (enabled: boolean) => {
    localStorage.setItem('fly-host-voice', enabled ? 'on' : 'off'); setHostVoice(enabled);
    if (!enabled) narrator.current.stop(); else if (!masterMuted) void narrator.current.unlock();
  };
  const changeAutoVoice = (enabled: boolean) => {
    void narrator.current.unlock();
    localStorage.setItem('fly-auto-voice', enabled ? 'on' : 'off');
    autoVoiceAttempt.current = '';
    setAutoVoice(enabled); setAutoVoiceError('');
    if (!enabled) voice.current?.disconnect();
  };
  useEffect(() => {
    if (!voiceAllowed || !room || !autoVoice || !config?.voice_configured) {
      autoVoiceAttempt.current = '';
      voice.current?.disconnect();
      return;
    }
    const key = room.id + ':' + room.state.id;
    if (autoVoiceAttempt.current === key) return;
    autoVoiceAttempt.current = key;
    void connectVoice();
  }, [voiceAllowed, autoVoice, config?.voice_configured, room?.id, room?.state.id, connectVoice]);

  const joinRoom = async () => {
    if (locked.current) return;
    locked.current=true; setBusy(true); setJoinError('');
    try {
      const [id, token] = invite.split('.');
      if (!id || !token) throw new Error('邀请链接不完整，请向房主重新获取');
      const next = await api<Room>('/api/rooms/' + encodeURIComponent(id) + '/join', { invite: token, name: joinName, profile_id:joinProfile });
      accept(next); setInvite(''); navigate('play');
    } catch (e) { setJoinError(e instanceof Error ? e.message : '入座失败'); }
    finally { locked.current=false; setBusy(false); }
  };
  const shareRoom = async () => {
    if (!room?.network) return;
    const link = location.origin + '/#join=' + room.id + '.' + room.network.invite;
    try { await navigator.clipboard.writeText(link); setToast('邀请链接已复制，发给朋友即可入座'); }
    catch { setModal('invite'); }
  };
  const ready = async (value: boolean) => {
    if (!room || busy) return;
    setBusy(true);
    try { accept(await api<Room>('/api/rooms/' + room.id + '/ready', { ready: value })); }
    catch(e) { setToast(e instanceof Error ? e.message : '准备失败'); }
    finally { setBusy(false); }
  };
  const discard = async () => {
    if (!room || locked.current) return;
    locked.current=true;setBusy(true);
    try {await api('/api/rooms/' + room.id + '/discard', {});voice.current?.disconnect();setModal(null);navigate('home');await load();}
    catch(e){setToast(e instanceof Error ? e.message : '删除失败，存档仍保留');}
    finally{locked.current=false;setBusy(false);}
  };
  const leave = async () => {
    if (!room || busy) return;
    try { await api('/api/rooms/' + room.id + '/leave', {}); voice.current?.disconnect(); navigate('home'); void load(); }
    catch(e) { setToast(e instanceof Error ? e.message : '离开失败'); }
  };
  if (invite && !loading) return <div className="loading-page join-page"><AirplaneTilt size={48} weight="fill" /><h1>朋友邀你一起飞</h1><p>输入昵称，领取自己的飞行员席位。原浏览器重新打开邀请会回到原席位。</p><form onSubmit={e => { e.preventDefault(); void joinRoom(); }}><ProfilePicker profiles={family.profiles} value={joinProfile} label="选择我的家庭档案" onChange={p=>{setJoinProfile(p?.id||'');if(p)setJoinName(p.name);}} /><label className="field-label" htmlFor="join-name">你的昵称</label><input id="join-name" className="room-name-input" maxLength={10} required value={joinName} onChange={e => {setJoinName(e.target.value);setJoinProfile('');}} /><button className="primary-button" disabled={busy}>加入房间 <ArrowRight /></button></form>{joinError && <p role="alert">{joinError}</p>}<button className="text-button" onClick={() => { setInvite(''); navigate('home'); }}>返回首页</button></div>;
  if (loading) return <div className="loading-page"><AirplaneTilt className="loading-plane" weight="fill" /><h1>正在准备家庭机场</h1><p>把棋盘铺好，快乐马上起飞。</p></div>;
  if (fatal || !room || !state || !current) return <div className="loading-page"><House size={48} /><h1>机场暂时还没连接上</h1><p>{fatal}</p><button className="primary-button" onClick={() => void load()}>重新连接 <ArrowClockwise /></button></div>;
  const winner = state.players.find(p => p.id === state.winner);
  const voiceLabels: Record<string, string> = { disconnected: '对讲麦克风已关闭', connecting: '正在连接', standby: '直接说话，我在听', ready: '直接说话，我在听', listening: '正在听你说', thinking: '正在理解', speaking: '灵矽正在说' };
  const effectiveVoice = voiceBusy ? room.voice : voiceState;
  return (
    <div className={'app-shell ' + (screen === 'play' ? 'play-shell' : 'home-shell')}>
      <a className="skip-link" href="#game-main" onClick={()=>document.querySelector<HTMLElement>('main')?.focus()}>跳到主要内容</a>
      <header className="site-header">
        <button className="brand brand-button" onClick={async () => { voice.current?.disconnect(); if (!network && lifecycle === 'playing' && state.phase !== 'finished') await command('pause'); navigate('home'); }} aria-label="返回首页"><span className="brand-mark"><AirplaneTilt size={26} weight="fill" /></span><span className="brand-name">一起飞<span>FlyTogether</span></span></button>
        <div className="header-actions">
          <button className="icon-button" aria-label="显示与操作设置" onClick={()=>setModal('experience')}><GearSix /></button>
          {screen === 'play' && <>
            {network && <button className="text-button" onClick={() => void shareRoom()}>邀请朋友</button>}
            {host && lifecycle === 'playing' && !winner && <button className="text-button" disabled={busy} onClick={() => void command('pause')}><Pause />暂停</button>}
            {host && lifecycle === 'paused' && <button className="text-button" disabled={busy} onClick={() => void command('resume')}><Play />继续游戏</button>}
            {host && lifecycle !== 'ended' && <button className="text-button" disabled={busy} onClick={() => setModal('end')} aria-label="结束游戏"><Stop /><span className="desktop-label">结束游戏</span><span className="mobile-label">结束</span></button>}
            <button className="icon-button" aria-label={masterMuted ? '恢复声音' : '总静音'} aria-pressed={masterMuted} onClick={() => changeMasterMute(!masterMuted)}>{!masterMuted ? <SpeakerHigh /> : <SpeakerSlash />}</button>
          </>}
          <button className="text-button" aria-label="新手指南" onClick={() => setTutorialOpen(true)}><Sparkle size={18} /><span className="desktop-label">新手指南</span><span className="mobile-label">新手</span></button>
          <button className="text-button rules-button" aria-label="玩法" onClick={() => setModal('rules')}><Question size={18} /><span>玩法</span></button>
        </div>
      </header>
      {screen === 'home' ? <main id="game-main" tabIndex={-1} className="home-main">
        <section className="home-welcome">
          <p className="eyebrow"><span /> A LITTLE FAMILY ADVENTURE</p>
          <h1>叫上家人，<br />一起飞一局<span>。</span></h1>
          <p className="home-description">一张棋盘，开口就能一起玩。<br />让每次起飞，都有人陪你欢呼。</p>
          <div className="home-actions"><button className="primary-button" onClick={() => openSetup(true)}><Plus size={21} />创建游戏<ArrowRight size={20} /></button>
            {(lifecycle === 'waiting' && network || lifecycle === 'playing' || lifecycle === 'paused') && state.phase !== 'finished' && <button className="outline-button" onClick={() => { navigate('play'); if (lifecycle === 'paused' && host) void command('resume'); }}>继续当前游戏<ArrowRight size={18} /></button>}
          </div>
          {room.saved_at && (lifecycle === 'playing' || lifecycle === 'paused' || lifecycle === 'waiting' && network) && state.phase !== 'finished' && <section className="saved-game" aria-label="已保存的对局"><strong>{room.restored ? '上次对局已恢复，等待继续' : room.name}</strong><p>已自动保存 · <time dateTime={room.saved_at}>{new Date(room.saved_at).toLocaleString('zh-CN')}</time></p><p>{room.restored ? '棋盘、骰子和道具已保留，继续后再开启语音。' : '刷新或服务重启后，可用当前浏览器回来继续。'}{config?.save_retention_hours ? `闲置存档保留 ${Math.round(config.save_retention_hours / 24)} 天。` : ''}</p>{host && <button className="text-button" onClick={() => setModal('discard')}>删除这份存档</button>}</section>}
          <button className="outline-button family-entry" onClick={()=>setModal('family')}>家庭飞行员与战绩 <ArrowRight size={18}/></button>
          <div className="home-features"><span>2—4 位飞行员</span><span>三种棋盘</span><span>语音游戏搭子</span></div>
        </section>
        <div className="home-flight-art" aria-hidden="true"><div className="home-orbit orbit-one" /><div className="home-orbit orbit-two" /><span className="home-cloud cloud-one" /><span className="home-cloud cloud-two" /><AirplaneTilt weight="fill" /><span className="home-flight-label">下一站，快乐到家。</span></div>
      </main> : <main id="game-main" tabIndex={-1} className="play-main">
        <div className="play-layout">
          <section className="board-panel" aria-label="对局棋盘与玩家">
            <div className="board-turnbar"><span style={{ color: palette[current.color] }}><AirplaneTilt weight="fill" />{winner ? `${winner.name}获胜` : `${current.name}的回合`}</span>
              <strong>{state.demo ? '演示局面' : state.board.name}{state.phase === 'move' ? ` · ${state.dice} 点` : ''}</strong>
              <div className="play-turn-actions">{lifecycle === 'playing' && !winner && state.phase === 'roll' && <button data-turn-control className="primary-button roll-button" disabled={!canAct} onClick={() => void command('roll')}>{current.ai ? '机器人回合' : busy ? (pendingAction === 'roll' ? (reducedMotion ? '正在掷骰…' : '骰子转呀转…') : '正在同步…') : '掷骰子'}</button>}
                {lifecycle === 'playing' && !current.ai && state.phase === 'move' && <button className="prop-shortcut coach-shortcut" disabled={!canAct} onClick={() => void command('coach', 'best')}><Sparkle weight="fill" />帮我选</button>}
                {lifecycle === 'playing' && !winner && state.phase === 'move' && state.legal_moves.map(move => <button key={move.piece_id} className="prop-shortcut" disabled={!canAct} data-turn-control title={move.summary} aria-pressed={preview?.piece_id === move.piece_id} onClick={() => selectMove(move.piece_id)}>{move.label}</button>)}
                {state.props_enabled && !winner && <button className="prop-shortcut" onClick={() => setModal('props')}>道具口袋 · {Object.values(current.props || {}).reduce((a, b) => a + b, 0)}</button>}
                {winner && <button className="prop-shortcut" onClick={() => setResultDismissed('')}>查看结算</button>}
              </div>
            </div>
            {lifecycle === 'playing' && !winner && !presentation.active && !presentation.queue.length && <p className="turn-help" role="status">{current.ai ? `${current.name}正在自动行动，稍等它走完。` : !myTurn ? `等待${current.name}操作，你可以看棋盘和听播报。` : state.phase === 'move' ? '点一架亮着的飞机，先看落点；确认后再飞行。' : current.pieces.every(piece => piece.progress < 0) && state.active_prop !== 'launch' ? `轮到${current.name}先掷骰子。机库飞机要掷到${state.preset === 'gentle' ? '五点或六点' : '六点'}才能起飞${state.props_enabled && current.props.launch > 0 ? '，也可以先用起飞卡' : ''}。` : `轮到${current.name}先掷骰子，再选择飞机。`}</p>}
            <details className="match-rules"><summary>本局规则 · {state.rules?.name || '自定义家庭规则'}</summary><p>{state.rules?.summary || '六点起飞；快速模式先到一架，完整模式全部抵达。'} 规则在本局固定。</p></details><div className={'board-viewport ' + (boardZoom ? 'is-zoomed' : '')}><div className="board-stage">{celebration && <Celebration key={celebration.id} text={celebration.text} won={celebration.won} reducedMotion={reducedMotion} onDismiss={()=>setCelebration(undefined)} />}{impact && <div className={"board-impact " + (impact.shield ? "shield-impact" : "bomb-impact")} role="status"><Sparkle weight="fill" /><strong>{impact.shield ? "护盾启动" : impact.crash ? "被撞回机场" : "炸弹出击"}</strong><span>{impact.text}</span></div>}{lifecycle !== 'playing' && <div className="game-status-overlay"><strong>{lifecycle === 'waiting' ? '飞行员准备好了吗？' : lifecycle === 'paused' ? '先休息一下' : '这一趟飞行已结束'}</strong><p>{lifecycle === 'waiting' ? (network ? '邀请朋友入座，所有人在线并准备后由房主开局。' : '确认玩家和难度后，点击开始游戏。') : lifecycle === 'paused' ? (room.restored ? '上次对局已恢复，骰子、道具和席位已保留，由房主确认继续。' : '棋盘、道具和骰子已保留，随时继续。') : '保留本局记录，重新准备下一次出发。'}</p>{host && <button className="primary-button" disabled={busy || !connected || (network && lifecycle === 'waiting' && !allReady)} onClick={() => lifecycle === 'ended' ? void command('replay') : void command(lifecycle === 'waiting' ? 'start' : 'resume')}><Play weight="fill" />{lifecycle === 'waiting' ? '开始游戏' : lifecycle === 'paused' ? '继续游戏' : '重新准备'}</button>}{network && !host && <p>等待房主{lifecycle === 'paused' ? '继续游戏' : '开始游戏'}，席位状态见下方准备区。</p>}</div>}<BoardLoader light={experience.board === 'flat'} reducedMotion={reducedMotion} onLight={()=>changeExperience({...experience,board:'flat'})} game={presentation.game || state} onPiece={onPiece} disabled={!canAct} onRoll={() => void command('roll')} rolling={busy && pendingAction === 'roll'} preview={preview} /></div></div>
            {network && <section className="lobby-panel" aria-label="联网准备区"><div><strong>{room.name} · 邀请房间</strong><span>你是{state.players.find(p => p.id === room.me?.player_id)?.name}{host ? '（房主）' : ''}</span></div><ul>{room.network!.seats.map((seat,i) => <li key={seat.player_id}><span style={{color: palette[state.players[i].color]}}>{seat.occupied ? state.players[i].name : '等待朋友入座'}{seat.robot && <span className="ai-tag">机器人</span>}{seat.player_id === room.me?.player_id ? ' · 我' : ''}</span><span>{seat.robot ? '自动准备 · 自动行动' : seat.occupied ? !seat.online ? '暂时离线 · 保留席位' : seat.ready ? '已准备' : '待准备' : '空席位'}</span></li>)}</ul><div className="lobby-actions"><button className="outline-button" onClick={() => void shareRoom()}>复制邀请链接</button>{lifecycle === 'waiting' && !host && <><button className="primary-button" disabled={busy || !connected} onClick={() => void ready(!room.network!.seats.find(s => s.player_id === room.me?.player_id)?.ready)}>{room.network!.seats.find(s => s.player_id === room.me?.player_id)?.ready ? '取消准备' : '我准备好了'}</button><button className="text-button" onClick={() => void leave()}>退出房间</button></>}</div>{lifecycle === 'waiting' && host && !allReady && <p>等待所有席位入座、在线并准备好。</p>}</section>}
            {room.save_error && <p role="alert" className="save-error">暂时无法保存，未确认的动作没有生效。请稍后重试后再继续游戏。</p>}
            {!connected && <p role="status" className="connection-hint">正在同步棋局，连接恢复后才可操作。</p>}
            <details className="keyboard-help"><summary>键盘操作</summary><p>Tab 切换控件，Enter 激活。焦点在棋盘主区域时：空格掷骰，1～4 选飞机，Enter 确认，Esc 取消，P 打开道具。</p></details>
            <div className="crew-list" style={{ '--seats': state.players.length } as React.CSSProperties}>
              {state.players.map((player, i) => <div key={player.id} className={'crew-seat ' + (state.current === i && state.phase !== 'finished' ? 'current' : '')} style={{ '--player-color': palette[player.color] } as React.CSSProperties}>
                <Avatar player={player} /><div className="crew-detail"><strong>{player.name}{player.ai && <span className="ai-tag">机器人</span>}</strong><span>{player.pieces.filter(p => p.progress === state.board.finish).length}/{state.mode === 'quick' ? '1' : player.pieces.length} 架抵达</span></div>{state.current === i && state.phase !== 'finished' && <span className="seat-turn">本回合</span>}
              </div>)}
            </div>
          </section>
          <aside className="side-column" aria-label="语音助手">
            {preview && <MovePreview game={state} move={preview} coach={previewCoach} onCoach={choice => void command('coach', choice)} disabled={!canAct} onCancel={() => { setPreviewSelection(undefined); if (previewCoach) void command('coach', 'cancel'); }} onConfirm={() => {
              const live = roomRef.current;
              if (previewCoach) {
                if (live?.coach?.id === previewCoach.id && live.state.id === previewCoach.game_id && live.state.version === previewCoach.version) void command('confirm_coach', previewCoach.id);
                return;
              }
              if (!live || live.state.id !== previewSelection?.gameID || live.state.version !== previewSelection.version || live.status !== 'playing') { setPreviewSelection(undefined); return; }
              void command('move', preview.piece_id);
            }} />}

            <section className="voice-panel">
              <div className="voice-heading"><span className="voice-icon"><Waveform size={24} /></span><div><h2>灵矽，陪你一起飞</h2><p>语音主持与助手 · 不占玩家席位</p></div><span className="voice-spark"><Sparkle size={22} weight="duotone" /></span></div>
              <label className="auto-voice-setting"><input type="checkbox" checked={hostVoice} onChange={e => { changeHostVoice(e.target.checked); }} />关闭麦克风时也播报回合</label>
              <HostNarration companion={companion} roomID={room.id} game={presentation.game || state} enabled={screen === 'play' && lifecycle === 'playing' && tutorialSeen && !tutorialOpen && hostVoice && !masterMuted && !!config?.voice_configured && voiceState === 'disconnected' && (network && !myTurn || !autoVoice || !!autoVoiceError || !!winner) && !presentation.active && !presentation.queue.length} narrator={narrator.current} onText={onAnnouncement} />
              <label className="auto-voice-setting"><input type="checkbox" checked={autoVoice} onChange={e => changeAutoVoice(e.target.checked)} />自动连接语音，直接说话</label>
              <div className="voice-bubble" aria-live="polite">{speech.text ? <><span className="speech-label">{speech.who === 'you' ? '你刚刚说' : '灵矽'}</span>{speech.text}</> : <><span className="bubble-quote">“</span>直接说「掷骰子」或<br />「让小熊号起飞」，我来帮你。</>}</div>
              <div className="voice-status"><span className={'status-dot ' + (voiceState === 'ready' ? 'online' : '')} />{network && !myTurn ? '当前玩家的回合，轮到你时可开口操作' : config?.voice_configured ? voiceLabels[effectiveVoice] || '等待连接' : '语音待配置 · 点击玩法已就绪'}</div>
              {voiceState === 'disconnected' || voiceState === 'connecting' ? <button className="voice-button" disabled={masterMuted || !myTurn || !connected || lifecycle !== 'playing' || !!winner || !config?.voice_configured || voiceState === 'connecting'} onClick={() => { changeAutoVoice(true); void connectVoice(); }}><Microphone size={20} />{masterMuted ? '总静音已开启' : lifecycle === 'paused' ? '继续游戏后自动连接语音' : lifecycle === 'ended' ? '新开一局后自动连接语音' : lifecycle !== 'playing' ? '开始游戏后自动连接语音' : voiceState === 'connecting' ? '正在准备麦克风…' : config?.voice_configured ? (autoVoiceError ? '点击启用麦克风并重试' : '开启自动语音') : '语音暂未开启'}</button> :
                <button className={'voice-button ' + (effectiveVoice === 'listening' ? 'listening' : '')} onClick={() => changeAutoVoice(false)}><Microphone size={21} />关闭对讲麦克风</button>}
              {autoVoiceError && <p className="voice-connection-error" role="alert">{autoVoiceError}</p>}
              {effectiveVoice === 'speaking' && <button className="stop-speech" onClick={() => voice.current?.stopPlayback()}>停止播报</button>}
              <button className="text-button" onClick={() => setModal('audio')}>声音设置</button><button className="text-button" onClick={() => setModal('companion')}>陪玩偏好</button>{masterMuted && <p role="status">总静音中，麦克风已关闭；恢复声音后沿用原来的设置。</p>}<div className="voice-examples"><strong>试着跟我说</strong><span>“掷骰子”</span><span>“使用起飞卡”</span><span>“让小熊号起飞”</span><span>“现在轮到谁”</span></div><p className="voice-footnote">{!config?.voice_configured ? '语音连接完成后，就能开口操作和询问规则。' : `${autoVoice ? '开始游戏后自动连接' : '开启语音后'}，直接说话即可。首次需允许麦克风；开启期间音频会发送给灵矽识别，可随时关闭。`}</p>{config?.diagnostics?.enabled && config.diagnostics.transcripts && <p className="voice-footnote">语音文本用于故障排查，保留{Math.ceil(config.diagnostics.retention_hours/24)}天，不保存录音。</p>}
            </section>
          </aside>
        </div>
      </main>}
      {tutorialOpen && !modal && !presentation.active && <Tutorial gentle={state.preset === 'gentle'} voiceConfigured={!!config?.voice_configured} propsEnabled={state.props_enabled} quick={state.mode === 'quick'} onClose={closeTutorial} />}
      {presentation.active?.kind === 'dice' && <DiceRollModal key={presentation.active.id} waitForConfirm={keyboardMode} reducedMotion={reducedMotion} roll={presentation.active.roll} onClose={() => { if (presentation.active) present({ type: 'advance', id: presentation.active.id }); }} />}
      {toast && <div className="toast" role="status"><Info size={19} /><span>{toast}</span><button aria-label="关闭提示" onClick={() => setToast('')}><X size={16} /></button></div>}
      {modal === 'experience' && <Modal title="显示与操作设置" onClose={()=>setModal(null)}><Suspense fallback={<p role="status">正在准备显示设置…</p>}><ExperienceSettings preferences={experience} onChange={changeExperience} systemReduced={systemReduced} onDone={()=>setModal(null)} /></Suspense></Modal>}
      {modal === 'family' && <Modal title="家庭飞行员与战绩" onClose={()=>setModal(null)}><FamilyPanel onChange={setFamily}/></Modal>}
      {modal === 'props' && <Modal title={`${current.name}的道具口袋`} onClose={() => setModal(null)}><PropTray key={state.id + ':' + state.version} game={state} status={lifecycle} disabled={!canAct} onUse={(id, target) => { setModal(null); void command('prop', id, target); }} /></Modal>}
      {modal === 'rules' && <Modal title="一起飞，怎么玩？" onClose={() => setModal(null)}><p className="modal-description">{state.rules?.summary || '快速模式先到一架，完整模式全部抵达。'}</p><div className="rules-list">{rules(state).map(([title, text], i) => <div key={title}><span>{String(i + 1).padStart(2, '0')}</span><section><h3>{title}</h3><p>{text}</p></section></div>)}</div><p className="rule-note">机库、起飞位和自己的终点航道不参与撞机。相同位置可以叠放；不使用三连六惩罚或终点反弹。</p><button className="primary-button" onClick={() => setModal(null)}>懂啦，一起出发 <Check /></button></Modal>}
      {modal === 'end' && <Modal title="结束这一局？" onClose={() => setModal(null)}><p className="modal-description">结束后停止当前对局并返回首页，可以创建新游戏。</p><button className="primary-button" disabled={busy} onClick={() => { setModal(null); void command('end'); }}>结束游戏 <Stop weight="fill" /></button></Modal>}
      {modal === 'companion' && <Modal title="灵矽陪玩偏好" onClose={() => setModal(null)}><p className="modal-description">选择适合家人的陪伴方式。只影响当前浏览器，不改变棋局或其他玩家的偏好。</p><label className="field-label" htmlFor="companion-style">陪玩风格</label><select id="companion-style" className="room-name-input" value={companion.style} onChange={e => changeCompanion({...companion, style:e.target.value as CompanionPreferences['style']})}><option value="warm">温暖陪伴 · 简短鼓励，不催促</option><option value="coach">耐心教练 · 适时补一条规则提示</option><option value="concise">简洁主持 · 只说结果与下一步</option></select><label className="field-label" htmlFor="companion-frequency">主动解说频率</label><select id="companion-frequency" className="room-name-input" value={companion.frequency} onChange={e => changeCompanion({...companion, frequency:e.target.value as CompanionPreferences['frequency']})}><option value="normal">每回合 · 提醒轮次与操作</option><option value="highlights">关键时刻 · 开局、跳跃、撞机与抵达</option><option value="quiet">只问才答 · 不主动插话</option></select><p className="rule-note">只问才答时，仍可开口掷骰、询问规则或请求建议。随时停止解说，也可以在声音设置中单独关麦或总静音。</p><button className="primary-button" onClick={() => setModal(null)}>完成偏好设置</button></Modal>}
      {modal === 'audio' && <Modal title="声音设置" onClose={() => setModal(null)}><p className="modal-description">分别控制游戏音效、回合播报和对讲麦克风。总静音会立即关闭全部声音和麦克风，保留你的偏好。</p><div className="audio-settings"><label><input type="checkbox" checked={sound} onChange={e => { setSound(e.target.checked); localStorage.setItem('fly-sound', e.target.checked ? 'on' : 'off'); }} />游戏音效（掷骰、起飞、撞机、抵达）</label><label><input type="checkbox" checked={hostVoice} onChange={e => changeHostVoice(e.target.checked)} />关闭麦克风时也播报回合</label><label><input type="checkbox" checked={autoVoice} onChange={e => changeAutoVoice(e.target.checked)} />自动连接语音，直接说话</label><label><input type="checkbox" checked={masterMuted} onChange={e => changeMasterMute(e.target.checked)} />总静音（包含麦克风）</label></div><p className="rule-note">回合播报独立于音效；对讲连接时，灵矽的回复随麦克风一起开启。引导期间保持安静。</p><button className="primary-button" onClick={() => setModal(null)}>完成设置</button></Modal>}
      {modal === 'discard' && <Modal title="删除这份存档？" onClose={() => setModal(null)}><p className="modal-description">删除后这局无法继续。{network ? '房间和所有玩家的席位也会一并关闭。' : '可以从首页创建新游戏。'}</p><button className="primary-button" disabled={busy} onClick={() => void discard()}>确认删除存档</button></Modal>}
      {modal === 'invite'  && room.network && <Modal title="邀请朋友入座" onClose={() => setModal(null)}><p>复制下面的链接发给朋友，每位朋友用自己的设备打开。</p><input aria-label="邀请链接" className="room-name-input" readOnly onFocus={e => e.target.select()} value={location.origin + '/#join=' + room.id + '.' + room.network.invite} /></Modal>}
      {winner && screen === 'play' && resultDismissed !== state.id && !presentation.active && !presentation.queue.length && !celebration && !modal && <Modal title="这次飞行，圆满抵达！" onClose={() => setResultDismissed(state.id)}><section className="match-result" aria-label="本局结算"><p className="result-winner">🏆 {winner.name}获胜</p><p>{state.board.name} · {state.mode === 'quick' ? '轻快一局' : '完整飞行'}{state.demo ? ' · 演示局，不计入战绩' : ' · 已保存到家庭飞行记录'}</p><div className="result-table"><table><caption>本局真实操作记录 · 幸运骰的两颗骰子算一次掷骰</caption><thead><tr><th>飞行员</th><th>掷骰</th><th>起飞</th><th>抵达</th><th>送回</th><th>道具</th></tr></thead><tbody>{state.players.map(p => <tr key={p.id}><th>{p.ai?'🤖':avatarIcon(p.avatar)} {p.name}{p.id === winner.id ? ' 🏆' : ''}</th><td>{p.stats?.rolls || 0}</td><td>{p.stats?.takeoffs || 0}</td><td>{p.stats?.arrivals || 0}</td><td>{p.stats?.captures || 0}</td><td>{p.stats?.props_used || 0}</td></tr>)}</tbody></table></div>{host ? <><button className="primary-button" disabled={busy || !connected} onClick={() => void command('replay')}>同配置再玩一局 <ArrowClockwise /></button><button className="text-button" onClick={() => openSetup(true)}>调整配置创建新房间</button></> : <p>等房主再开一局，原席位会为你保留。</p>}<button className="outline-button" onClick={() => { setResultDismissed(state.id); navigate('home'); }}>返回首页 <House /></button></section></Modal>}
      {modal === 'setup' && <Modal title={setupNew ? '创建家庭房间' : '准备下一次出发'} onClose={() => setModal(null)}>
        <form onSubmit={e => { e.preventDefault(); void restart(false); }}>
          <p className="modal-description">选好玩家、路线和难度，同屏模式直接进入棋盘；联网模式先进入准备区，分享链接让朋友自行填写昵称。</p>
          <label className="toggle-row"><span><strong>多人联网邀请房间</strong><small>每人一台设备 · 免注册 · 房主开局</small></span><input type="checkbox" checked={!!options.network} onChange={e => setOptions(o => ({...o, network:e.target.checked}))} /></label>
          <label className="field-label" htmlFor="room-name">房间名称</label><input id="room-name" className="room-name-input" value={options.room_name} maxLength={20} required onChange={e => setOptions(o => ({...o, room_name: e.target.value}))} />
          <label className="field-label" htmlFor="rule-preset">玩法预设</label><select id="rule-preset" className="room-name-input" value={options.preset || 'custom'} onChange={e => {const preset=e.target.value as Options['preset'];setOptions(o=>({...o,preset,mode:preset==='classic'?'classic':'quick',props:preset!=='classic'}));}}><option value="custom">自定义家庭规则 · 自选节奏与道具</option><option value="gentle">亲子轻松 · 5或6起飞，辅助道具</option><option value="classic">经典飞行 · 6点起飞，四架全部抵达</option></select><p className="preset-summary">{options.preset==='gentle'?'每人2架，先到1架；仅6再掷。起飞卡2、加速卡1、幸运骰1、护盾2，无炸弹。':options.preset==='classic'?'每人4架全部抵达获胜，6点起飞并可再掷，无道具。':'6点起飞并可再掷；下面选择飞机数量和道具。'} 开局后固定。</p>
          <label className="field-label">机器人难度</label><div className="difficulty-options">{([{id:'easy',name:'轻松',desc:'随机选子，不使用道具'},{id:'normal',name:'普通',desc:'优先好走法，使用辅助道具'},{id:'hard',name:'挑战',desc:'规划走法，主动攻击与防守'}] as const).map(level => <button key={level.id} type="button" aria-pressed={options.difficulty === level.id} className={options.difficulty === level.id ? 'selected' : ''} onClick={() => setOptions(o => ({...o,difficulty:level.id}))}><strong>{level.name}</strong><span>{level.desc}</span></button>)}</div><p className="wake-help">难度只影响机器人走法，不改变随机骰子和真人规则；未添加机器人时不生效。</p>
          <label className="field-label">棋盘路线</label><div className="board-options">{(config?.boards || []).map(board => <button type="button" key={board.id} className={options.board === board.id ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, board: board.id }))}><strong>{board.name}</strong><span>{board.ring_length} 个航线格</span><small>{board.home_length} 格终点跑道</small></button>)}</div>
          <label className="field-label">总席位（含机器人）</label><div className="segmented">{[2, 3, 4].map(count => <button type="button" key={count} className={options.names.length === count ? 'selected' : ''} onClick={() => setOptions(o => {
            const used = new Set<string>();
            const names = Array.from({ length: count }, (_, i) => {
              let name = o.ai && i === count - 1 ? '灵矽机器人' : (o.names[i] && !['灵矽', '灵矽机器人'].includes(o.names[i]) ? o.names[i] : ['小朋友', '爸爸', '妈妈', '家人'][i]);
              if (used.has(name)) name = ['小朋友', '爸爸', '妈妈', '家人', '飞行员'].find(candidate => !used.has(candidate))!;
              used.add(name); return name;
            });
            return { ...o, names, profile_ids:names.map((name,i)=>name===o.names[i]?o.profile_ids?.[i]||'':''),avatars:names.map((name,i)=>name===o.names[i]?o.avatars?.[i]||'':'') };
          })}>{count} 位</button>)}</div>
          <p className="seat-composition" role="status">{options.names.length - (options.ai ? 1 : 0)} 位真人{options.ai ? ' + 1 位机器人' : ''} · 共 {options.names.length} 个席位{options.network ? '，朋友通过邀请链接自行填写昵称' : ''}</p>
          <div className="name-fields">{options.names.map((name, i) => <label key={i}><span className="name-color" style={{ background: palette[['coral', 'gold', 'blue', 'jade'][i]] }} /><span className="sr-only">第 {i + 1} 位飞行员昵称</span><input maxLength={10} required disabled={!!options.network && i > 0} value={name} onChange={e => setOptions(o => ({ ...o, names: o.names.map((v, n) => n === i ? e.target.value : v),profile_ids:o.names.map((_,n)=>n===i?'':o.profile_ids?.[n]||''),avatars:o.names.map((_,n)=>n===i?'':o.avatars?.[n]||'') }))} />{options.ai && i === options.names.length - 1 && <Robot size={20} />}</label>)}</div>
          <div className="setup-profiles">{options.names.map((_,i)=>(!options.network||i===0)&&!(options.ai&&i===options.names.length-1)&&<label key={i}><span>第 {i+1} 位 · 复用档案</span><ProfilePicker profiles={family.profiles} value={options.profile_ids?.[i]||''} label={`第 ${i+1} 位家庭档案`} onChange={p=>setOptions(o=>({...o,names:o.names.map((v,n)=>n===i&&p?p.name:v),profile_ids:o.names.map((_,n)=>n===i?p?.id||'':o.profile_ids?.[n]||''),avatars:o.names.map((_,n)=>n===i?p?.avatar||'':o.avatars?.[n]||'')}))}/></label>)}</div><p className="wake-help">在首页“家庭飞行员与战绩”保存昵称和头像。选档案后修改昵称会改为临时飞行员。</p>
          <label className="toggle-row"><span><strong>添加机器人陪玩（占一个席位）</strong><small>最后一席自动准备、掷骰和走棋，同屏与联网均可用</small></span><input type="checkbox" checked={options.ai} onChange={e => setOptions(o => ({ ...o, ai: e.target.checked, profile_ids:o.names.map((_,i)=>i===o.names.length-1?'':o.profile_ids?.[i]||''),avatars:o.names.map((_,i)=>i===o.names.length-1?'':o.avatars?.[i]||''), names: o.names.map((name, i) => i === o.names.length - 1 ? (e.target.checked ? '灵矽机器人' : ['小朋友', '爸爸', '妈妈', '家人'][i]) : name) }))} /></label>
          <p className="wake-help">灵矽语音助手不占席位，始终可按偏好开启，负责主持、讲解和帮助真人操作；机器人是自动参与对局的玩家。</p>
          <label className="toggle-row"><span><strong>趣味道具模式</strong><small>{options.preset==='gentle'?'亲子辅助补给，无炸弹':options.preset==='classic'?'经典飞行不使用道具':'每人五张道具：起飞、加速、幸运、炸弹、护盾'}</small></span><input type="checkbox" disabled={!!options.preset && options.preset !== 'custom'} checked={options.props} onChange={e => setOptions(o => ({ ...o, props: e.target.checked }))} /></label>
          <label className="field-label">这一局的节奏</label><div className="mode-options"><button type="button" disabled={!!options.preset && options.preset !== 'custom'} className={options.mode === 'quick' ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, mode: 'quick' }))}><Sparkle /><strong>轻快一局</strong><span>每人 2 架 · 先到 1 架获胜</span></button><button type="button" disabled={!!options.preset && options.preset !== 'custom'} className={options.mode === 'classic' ? 'selected' : ''} onClick={() => setOptions(o => ({ ...o, mode: 'classic' }))}><AirplaneTilt /><strong>完整飞行</strong><span>每人 4 架 · 全部抵达获胜</span></button></div>
          <button className="primary-button" disabled={busy} type="submit">{busy ? '准备机场中…' : (options.network ? '创建邀请房间' : setupNew ? '创建并开始游戏' : '开始新的一局')}<ArrowRight /></button>
          <button type="button" disabled={busy || !!options.network} className="demo-link" onClick={() => void restart(true)}>试试预设演示局面 <ArrowUpRight size={14} /></button>
        </form>
      </Modal>}
    </div>
  );
}
