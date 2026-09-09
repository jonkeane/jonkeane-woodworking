const { test } = require('node:test');
const assert = require('node:assert/strict');
const { readFileSync } = require('node:fs');
const { runInNewContext } = require('node:vm');

const source = readFileSync(`${__dirname}/../assets/js/photo-swiper.js`, 'utf8')
  .replace(/^import .*;\n/gm, '');

let lastSwiper;
class FakeSwiper {
  constructor(viewer, options) {
    this.viewer = viewer;
    this.options = options;
    this.handlers = {};
    this.activeIndex = options.initialSlide;
    this.allowTouchMove = true;
    this.initialized = false;
    this.animating = false;
    this.zoom = { out() {} };
    lastSwiper = this;
  }

  on(names, callback) {
    for (const name of names.split(' ')) this.handlers[name] = callback;
  }

  emit(name, ...args) {
    this.handlers[name]?.(...args);
  }

  slideTo(index) {
    this.activeIndex = index;
  }

  init() {
    this.initialized = true;
  }

  update() {}
}

function photoPage() {
  lastSwiper = undefined;
  const visits = [];
  const windowHandlers = {};
  const documentHandlers = {};
  const documentClasses = new Set();
  const slides = [
    { dataset: { photoUrl: '/previous/' } },
    { dataset: { current: 'true', photoUrl: '/current/' } },
    { dataset: { photoUrl: '/next/' } },
  ];
  let focused = false;
  const viewer = {
    addEventListener: (name, callback) => { documentHandlers[`viewer:${name}`] = callback; },
    focus: () => { focused = true; },
    querySelectorAll: () => slides,
    setAttribute() {},
  };
  const document = {
    querySelector: (selector) => selector === '.photo-swiper' ? viewer : null,
    addEventListener: (name, callback) => { documentHandlers[name] = callback; },
    documentElement: {
      classList: { toggle: (name, enabled) => enabled ? documentClasses.add(name) : documentClasses.delete(name) },
    },
  };
  const location = { href: 'https://example.test/current/', hash: '' };
  const window = {
    addEventListener: (name, callback) => { windowHandlers[name] = callback; },
    dispatchEvent: (event) => windowHandlers[event.type]?.(event),
    location: { ...location, assign: (url) => visits.push(url) },
    matchMedia: () => ({ matches: false }),
    visualViewport: { scale: 1, addEventListener() {} },
  };
  const history = {
    state: null,
    replaceState: (_state, _unused, url) => {
      window.location.href = url.href;
      window.location.hash = url.hash;
    },
  };

  runInNewContext(source, {
    document, Event, history, Mousewheel: {}, Swiper: FakeSwiper, URL, window, Zoom: {},
  });
  return { documentClasses, documentHandlers, focused: () => focused, slides, swiper: lastSwiper, visits, windowHandlers };
}

function drawerPage() {
  const listeners = {};
  const drawerListeners = {};
  const classes = new Set();
  const summary = { addEventListener: (name, callback) => { listeners[name] = callback; } };
  const details = { open: true, querySelector: () => summary };
  const drawer = {
    querySelector: () => details,
    classList: {
      add: (name) => classes.add(name),
      remove: (name) => classes.delete(name),
    },
    addEventListener: (name, callback) => { drawerListeners[name] = callback; },
  };
  const document = {
    querySelector: (selector) => selector === '.photo-drawer' ? drawer : null,
  };
  const window = { matchMedia: () => ({ matches: false }) };

  runInNewContext(source, { document, window, Mousewheel: {}, Swiper: FakeSwiper, Zoom: {} });
  return { classes, details, drawerListeners, listeners };
}

test('Swiper starts on the current photo and permits both adjacent directions', () => {
  const page = photoPage();
  assert.equal(page.swiper.options.initialSlide, 1);
  assert.equal(page.swiper.allowSlidePrev, true);
  assert.equal(page.swiper.allowSlideNext, true);
  assert.equal(page.swiper.options.simulateTouch, true);
  assert.equal(page.swiper.options.grabCursor, true);
  assert.equal(page.swiper.options.mousewheel.enabled, true);
  assert.equal(page.swiper.options.mousewheel.forceToAxis, true);
});

test('double-clicking toggles image-only view and preserves it while navigating', () => {
  const page = photoPage();
  page.swiper.emit('doubleTap');
  assert.equal(page.documentClasses.has('photo-only'), true);
  assert.equal(page.focused(), true);
  page.swiper.activeIndex = 2;
  page.swiper.emit('slideChangeTransitionStart');
  page.swiper.emit('slideChangeTransitionEnd');
  assert.deepEqual(page.visits, ['https://example.test/next/#photo-only']);
});

test('completing a slide transition visits the adjacent photo once', () => {
  const page = photoPage();
  page.swiper.activeIndex = 2;
  page.swiper.emit('slideChangeTransitionStart');
  page.swiper.emit('slideChangeTransitionEnd');
  page.swiper.emit('slideChangeTransitionEnd');
  assert.deepEqual(page.visits, ['/next/']);
  assert.equal(page.swiper.allowTouchMove, false);
});

test('zoomed gestures return to the current slide instead of navigating', () => {
  const page = photoPage();
  page.swiper.emit('zoomChange', null, 2);
  page.swiper.activeIndex = 2;
  page.swiper.emit('slideChangeTransitionStart');
  page.swiper.emit('slideChangeTransitionEnd');
  assert.equal(page.swiper.activeIndex, 1);
  assert.deepEqual(page.visits, []);
});

test('closing details waits for the drawer animation', () => {
  const page = drawerPage();
  let prevented = false;
  page.listeners.click({ preventDefault: () => { prevented = true; } });
  assert.equal(prevented, true);
  assert.equal(page.classes.has('is-closing'), true);
  assert.equal(page.details.open, true);
  page.drawerListeners.animationend({ animationName: 'photo-details-down' });
  assert.equal(page.classes.has('is-closing'), false);
  assert.equal(page.details.open, false);
});
