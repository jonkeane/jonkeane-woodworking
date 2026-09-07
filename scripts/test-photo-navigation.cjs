const { test } = require('node:test');
const assert = require('node:assert/strict');
const { runInNewContext } = require('node:vm');
const { readFileSync } = require('node:fs');
const source = readFileSync(`${__dirname}/../assets/js/photo-navigation.js`, 'utf8');

function page() {
  let now = 0;
  const handlers = {};
  const advance = (time) => { now = time; };
  const visits = [];
  const photo = { clientWidth: 800, addEventListener() {} };
  runInNewContext(source, {
    Date: { now: () => now },
    document: { querySelector: selector => selector.includes('photo-full') ? photo : { href: selector.includes('next') ? '/next' : '/prev' } },
    window: {
      addEventListener: (name, callback) => { handlers[name] = callback; },
      location: { assign: url => visits.push(url) },
    },
  });
  return {
    visits,
    advance,
    wheel(time, deltaX = 60, options = {}) {
      advance(time);
      handlers.wheel({ target: photo, deltaX, deltaY: 0, deltaMode: 0, preventDefault() {}, ...options });
    },
    restore(time) { advance(time); handlers.pageshow(); },
  };
}

function drawerPage() {
  const listeners = {};
  const drawerListeners = {};
  const classes = new Set();
  const drawer = {
    classList: {
      add: name => classes.add(name),
      remove: name => classes.delete(name),
    },
    addEventListener: (name, callback) => { drawerListeners[name] = callback; },
  };
  const details = { open: true, querySelector: selector => selector === 'summary' ? summary : null };
  const summary = { addEventListener: (name, callback) => { listeners[name] = callback; } };
  drawer.querySelector = selector => selector === 'details' ? details : null;
  runInNewContext(source, {
    document: { querySelector: selector => selector === '.photo-drawer' ? drawer : null },
    window: { matchMedia: () => ({ matches: false }) },
  });
  return { classes, details, drawerListeners, listeners };
}

test('one large gesture navigates once', () => {
  const p = page();
  for (let time = 700; time < 2500; time += 50) p.wheel(time);
  assert.deepEqual(p.visits, ['/next']);
});

test('momentum on the destination must stop before a new swipe is accepted', () => {
  const p = page();
  for (let time = 50; time < 2500; time += 50) p.wheel(time);
  assert.deepEqual(p.visits, []);
  p.wheel(3000, -60);
  assert.deepEqual(p.visits, ['/prev']);
});

test('restoring a page also rejects continuing momentum', () => {
  const p = page();
  p.wheel(700);
  p.restore(1000);
  for (let time = 1050; time < 2500; time += 50) p.wheel(time);
  assert.deepEqual(p.visits, ['/next']);
  p.wheel(3000);
  assert.deepEqual(p.visits, ['/next', '/next']);
});


test('navigation starts immediately when the distance threshold is crossed', () => {
  const p = page();
  p.wheel(200, 30);
  assert.deepEqual(p.visits, []);
  p.wheel(250, 30);
  assert.deepEqual(p.visits, ['/next']);
});

test('small gestures do not navigate', () => {
  const p = page();
  p.wheel(200, 20);
  p.advance(400);
  assert.deepEqual(p.visits, []);
});


test('zoom and vertical scrolling do not navigate', () => {
  const p = page();
  p.wheel(200, 100, { ctrlKey: true });
  p.wheel(400, 100, { deltaY: 150 });
  assert.deepEqual(p.visits, []);
});

test('closing details waits for the drawer animation', () => {
  const p = drawerPage();
  let prevented = false;
  p.listeners.click({ preventDefault: () => { prevented = true; } });
  assert.equal(prevented, true);
  assert.equal(p.classes.has('is-closing'), true);
  assert.equal(p.details.open, true);
  p.drawerListeners.animationend({ animationName: 'photo-details-down' });
  assert.equal(p.classes.has('is-closing'), false);
  assert.equal(p.details.open, false);
});
