import {useState} from 'react';
import type {Room} from '../lib/types';
import {api} from '../lib/types';

type Props={room:Room;connected:boolean;busy:boolean;onBusy:(v:boolean)=>void;onRoom:(r:Room)=>void};
export function DelegationControls({room,connected,busy,onBusy,onRoom}:Props){
 const seat=room.network?.seats.find(s=>s.player_id===room.me?.player_id);
 const [confirm,setConfirm]=useState(''),[error,setError]=useState('');
 if(!seat)return null;
 const active=room.status!=='ended'&&room.state.phase!=='finished';
 const apply=async()=>{
  if(!confirm||busy||!connected)return;
  onBusy(true);setError('');
  const body={command_id:crypto.randomUUID(),game_id:room.state.id,expected_version:room.state.version,action:confirm};
  try{
   let next:Room;
   try{next=await api<Room>('/api/rooms/'+room.id+'/commands',body)}catch(e){if(e instanceof TypeError)next=await api<Room>('/api/rooms/'+room.id+'/commands',body);else throw e}
   onRoom(next);setConfirm('');
  }catch(e){const fresh=await api<{room:Room|null}>('/api/session').catch(()=>null);if(fresh?.room?.id===room.id)onRoom(fresh.room);setError(e instanceof Error?e.message:'托管设置未完成，请重试')}
  finally{onBusy(false)}
 };
 return <section className="delegation-controls" aria-label="我的暂离托管">
  <h3>我的暂离托管</h3>
  <p>{!active?'本局已结束，下次再玩默认恢复本人操作。':seat.delegated?'正在由托管代玩。回来后请确认接管，才恢复点击与对讲操作。':'当前由你本人操作。需要离开时，可以让机器人暂时替你走棋。'} 托管只掷骰与走棋，不使用你的道具，也不会转让房主。</p>
  {active&&<><div className="delegation-actions"><button className="outline-button" disabled={!connected||busy||(!seat.delegated&&room.status==='waiting')} onClick={()=>{setConfirm(seat.delegated?'reclaim':'delegate');setError('')}}>{seat.delegated?'回来接管':'暂离并托管'}</button>
  <button className="text-button" disabled={!connected||busy} onClick={()=>{setConfirm(seat.auto_delegate?'auto_delegate_off':'auto_delegate_on');setError('')}}>{seat.auto_delegate?'关闭断线托管':'允许断线后托管'}</button></div>
  <p className="rule-note">断线托管：{seat.auto_delegate?'已允许':'未允许（默认）'}。只有你提前允许，并持续断线约{room.network?.delegation_seconds||60}秒后才会开始；其他标签页仍在线或等待期内重连，会取消本次等待。所有人都离线时停止代玩。</p></>}
  {confirm&&active&&<section className="host-confirm" aria-label="确认暂离托管"><h3>{confirm==='delegate'?'确认暂离并托管？':confirm==='reclaim'?'确认回来接管？':confirm==='auto_delegate_on'?'允许断线后自动托管？':'关闭断线托管？'}</h3>
   <p>{confirm==='delegate'?'即刻交给托管，直到你明确回来接管。已掷出的骰子会保留，不会重新投掷。':confirm==='reclaim'?'确认成功后立即由你本人操作。若托管刚刚完成一步，以最新棋盘为准，不会撤回或重复走棋。':confirm==='auto_delegate_on'?'以后本局持续断线达到等待期，且有真人仍在房间时，托管会自动替你掷骰与走棋，不使用道具。':'关闭后，今后断线将保留席位等待你回来；如果当前已经托管，仍需单独点击“回来接管”。'}</p>
   <div><button className="outline-button" disabled={busy} onClick={()=>setConfirm('')}>先不调整</button><button className="primary-button" disabled={!connected||busy} onClick={()=>void apply()}>{busy?'正在保存…':'确认'+(confirm==='delegate'?'暂离托管':confirm==='reclaim'?'回来接管':confirm==='auto_delegate_on'?'允许断线托管':'关闭断线托管')}</button></div></section>}
  {error&&<p className="room-management-error" role="alert">{error}</p>}
  <p className="rule-note">暂停或服务重启后不会继续代玩，需房主继续游戏。同配置再玩会清除本局托管与断线托管选择。</p>
 </section>
}
