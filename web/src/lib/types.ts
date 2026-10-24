export type Piece = { id: string; label: string; number: number; progress: number; shield?: boolean };
export type MatchStats = { rolls: number; takeoffs: number; arrivals: number; captures: number; props_used: number };
export type Player = { avatar?: string; stats: MatchStats; id: string; name: string; color: string; ai: boolean; pieces: Piece[]; props: Record<string, number> };
export type Move = {
  piece_id: string; label: string; from: number; to: number; captured: string[]; blocked?: string[];
  jump: boolean; fly: boolean; launch: boolean; finish: boolean; summary: string;
};
export type PropEffect = {piece_id:string;from:number;to:number;kind:string};
export type GameEvent = { prop_id?:string; effects?:PropEffect[]; id: string; kind: string; text: string; player_id?: string; piece_id?: string; dice_rolls?: number[] };
export type BoardSpec = {
  id: string; name: string; layout: 'cross' | 'circle'; ring_length: number;
  home_length: number; finish: number; jump_length: number; fly_from: number; fly_to: number;
};
export type Game = {
  id: string; preset?: 'custom' | 'gentle' | 'classic'; rules?: {id:string;name:string;summary:string;launch_dice:number[]}; ruleset: string; difficulty: 'easy' | 'normal' | 'hard'; board: BoardSpec; mode: string; demo: boolean; version: number; turn: number;
  theme_id?: string; mission?: {id:string;title:string;description:string;progress:number;target:number;contributors:string[];status:'available'|'active'|'completed'|'incomplete'};
  current: number; phase: 'roll' | 'move' | 'finished'; dice: number; winner?: string;
  players: Player[]; legal_moves: Move[]; history: GameEvent[]; last_move?: Move;
  props_enabled: boolean; active_prop?: string; dice_rolls: number[];
};
export type CompanionPreferences = { style: 'warm' | 'coach' | 'concise'; frequency: 'normal' | 'highlights' | 'quiet' };
export type VoiceSettings = { wake_enabled: boolean; wake_word: string };
export type Coach = { id: string; game_id: string; version: number; player_id: string; move: Move; reason: string; risk: string; threats: number };
export type ChatMessage = { id: string; at: string; from_id: string; name: string; role: 'player' | 'host' | 'spectator'; kind: 'emoji' | 'text'; emoji?: string; text?: string; target_player_id?: string };
export type Audience = { enabled: boolean; token?: string; count: number; online: number; capacity: number; chat_enabled: boolean; me?: { id: string; name: string; role: 'spectator' }; messages?: ChatMessage[] };
export type Room = { host_change?: {id:string;from:string;to:string;reason:string;at:string}; profile_ids?: string[]; instance_id?: string; saved_at?: string; restored?: boolean; save_error?: boolean; id: string; name: string; status: 'waiting' | 'playing' | 'paused' | 'ended'; seq: number; state: Game; voice: string; voice_settings: VoiceSettings; coach?: Coach; coach_expired?: boolean; audience?: Audience; network?: { invite: string; host_player_id?:string;host_online?:boolean;host_claim_at?:string;host_claim_wait_seconds?:number;can_claim_host?:boolean; delegation_seconds?:number; seats: { delegated?:boolean;auto_delegate?:boolean;delegation_reason?:string;delegate_wait_seconds?:number; host?:boolean; player_id: string; occupied: boolean; ready: boolean; online: boolean; robot?: boolean }[] }; me?: { player_id: string; host: boolean } };
export type MissionCard = {id:string;title:string;description:string;status:'available'};
export type Options = { profile_ids?: string[]; avatars?: string[]; preset?: 'custom' | 'gentle' | 'classic'; network?: boolean; room_name: string; difficulty: 'easy' | 'normal' | 'hard'; waiting: boolean; names: string[]; board: string; mode: string; theme_id?:string; mission_id?:string; ai: boolean; demo: boolean; props: boolean };
export type Config = { delegation_seconds?:number; host_reclaim_seconds?:number; diagnostics?: {enabled:boolean;transcripts:boolean;retention_hours:number}; persistence_enabled?: boolean; save_retention_hours?: number; voice_configured: boolean; version: string; ruleset: string; boards: BoardSpec[]; missions?:MissionCard[] };

export class APIError extends Error {
  constructor(public status: number, public code: string, message: string) { super(message); }
}
export async function api<T>(path: string, body?: unknown): Promise<T> {
  const response = await fetch(path, {
    method: body === undefined ? 'GET' : 'POST',
    credentials: 'same-origin',
    headers: body === undefined ? {} : { 'Content-Type': 'application/json' },
    body: body === undefined ? undefined : JSON.stringify(body),
    signal: AbortSignal.timeout(12000),
  });
  const value = await response.json();
  if (!response.ok) throw new APIError(response.status, value.code, value.message || '连接暂时没有成功，请再试一次');
  return value as T;
}
export const socketURL = (path: string) => (location.protocol === 'https:' ? 'wss://' : 'ws://') + location.host + path;
export const colors = ['coral', 'gold', 'blue', 'jade'];
export const palette: Record<string, string> = { coral: '#de594f', gold: '#cd901f', blue: '#387fc0', jade: '#328760' };

export type Profile = { id: string; name: string; avatar: string };
export type MatchResult = { game_id: string; completed_at: string; room_name: string; board: string; mode: string; preset: string; difficulty: string; network: boolean; props: boolean; players: { profile_id?: string; name: string; avatar?: string; ai: boolean; won: boolean; stats: MatchStats }[] };
export type Family = { profiles: Profile[]; matches: MatchResult[]; total: number };
