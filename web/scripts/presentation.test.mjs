import { test } from 'node:test';
import assert from 'node:assert/strict';
import { initialPresentation, presentationReducer as reduce } from '../src/lib/presentation.ts';
import { cuesFor } from '../src/lib/sfx.ts';

const event=(id,kind,text='',extra={})=>({id,kind,text,...extra});
const rolled=id=>event(id,'rolled','小朋友掷出了 2 点',{dice_rolls:[2]});
const launched=id=>event(id,'moved','小朋友的小熊号：从机场起飞，进入起飞位',{piece_id:'p0-0'});
const moved=id=>event(id,'moved','小朋友的小熊号：前进 4 格',{piece_id:'p0-0'});
const game=(version,progress,history,id='match')=>({id,version,history,players:[{pieces:[{id:'p0-0',progress}]}]});
const sync=(p,g,status='playing')=>reduce(p,{type:'sync',game:g,status});
const next=p=>reduce(p,{type:'advance',id:p.active.id});
const position=p=>p.game.players[0].pieces[0].progress;

test('automatic launch stays in hangar and stays silent until its dice popup closes',()=>{
 const start=game(1,-1,[]);let p=sync(initialPresentation,start);
 const committed=game(2,0,[rolled('roll'),launched('launch')]);p=sync(p,committed);
 assert.equal(committed.players[0].pieces[0].progress,0,'authoritative result is already committed');
 assert.equal(p.active.kind,'dice');assert.equal(position(p),-1);assert.deepEqual(cuesFor(p.effect.events),['roll']);
 p=next(p);assert.equal(position(p),0);assert.equal(p.active.kind,'board');assert.deepEqual(cuesFor(p.effect.events),['launch']);
 p=next(p);assert.equal(p.active,undefined);assert.equal(position(p),0);
});
test('voice or AI selection received while popup is open also waits, then launches once',()=>{
 let p=sync(initialPresentation,game(1,-1,[]));
 p=sync(p,game(2,-1,[rolled('roll')]));const effect=p.effect;
 p=sync(p,game(3,0,[rolled('roll'),launched('launch')]));
 assert.equal(position(p),-1);assert.equal(p.effect,effect);assert.equal(p.active.kind,'dice');
 p=next(p);assert.equal(position(p),0);assert.deepEqual(cuesFor(p.effect.events),['launch']);
 const effectAfter=p.effect;p=sync(p,game(3,0,[rolled('roll'),launched('launch')]));
 assert.equal(p.effect,effectAfter);assert.equal(p.queue.length,0,'duplicate snapshots must not launch twice');
});
test('consecutive AI dice cannot skip or cover the flight belonging to the preceding popup',()=>{
 let p=sync(initialPresentation,game(1,-1,[]));
 p=sync(p,game(2,0,[rolled('roll-1'),launched('launch')]));
 p=sync(p,game(3,4,[rolled('roll-1'),launched('launch'),rolled('roll-2'),moved('move')]));
 assert.equal(position(p),-1);assert.equal(p.active.id,'roll-1');
 p=next(p);assert.equal(position(p),0);assert.equal(p.active.kind,'board');assert.deepEqual(cuesFor(p.effect.events),['launch']);
 p=next(p);assert.equal(position(p),0);assert.equal(p.active.id,'roll-2');assert.deepEqual(cuesFor(p.effect.events),['roll']);
 p=next(p);assert.equal(position(p),4);assert.deepEqual(cuesFor(p.effect.events),['hop']);
});
test('pause, end and a new match discard unfinished visual and sound beats',()=>{
 for(const status of ['paused','ended','waiting']){
  let p=sync(initialPresentation,game(1,-1,[]));p=sync(p,game(2,0,[rolled('roll'),launched('launch')]));
  p=sync(p,game(3,0,[rolled('roll'),launched('launch')]),status);
  assert.equal(p.active,undefined);assert.equal(p.effect,undefined);assert.equal(p.queue.length,0);
  assert.equal(reduce(p,{type:'advance',id:'roll'}),p);
 }
 let p=sync(initialPresentation,game(1,-1,[]));p=sync(p,game(2,0,[rolled('roll'),launched('launch')]));
 p=sync(p,game(1,-1,[],'new-match'));assert.equal(position(p),-1);assert.equal(p.active,undefined);assert.equal(p.effect,undefined);
});
test('a passing roll releases the popup without creating a flight cue',()=>{
 let p=sync(initialPresentation,game(1,-1,[]));p=sync(p,game(2,-1,[rolled('roll'),event('pass','passed')]));p=next(p);
 assert.equal(p.active,undefined);assert.equal(position(p),-1);assert.deepEqual(cuesFor(p.effect.events),[]);
});
test('winning movement and its fanfare wait for the popup too',()=>{
 let p=sync(initialPresentation,game(1,56,[]));
 p=sync(p,game(2,58,[rolled('roll'),event('home','moved','小熊号抵达终点'),event('win','won','获胜')]));
 assert.equal(position(p),56);assert.deepEqual(cuesFor(p.effect.events),['roll']);
 p=next(p);assert.equal(position(p),58);assert.deepEqual(cuesFor(p.effect.events),['win']);
});
