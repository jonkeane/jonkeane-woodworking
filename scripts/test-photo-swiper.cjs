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
    this.zoom = {
      scale: 1,
      in: (scale) => { this.emit('zoomChange', this, scale); this.zoom.scale = scale; },
      out: () => { this.emit('zoomChange', this, 1); this.zoom.scale = 1; },
    };
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
  const listenerOptions = {};
  const resizeObservers = [];
  const strip = {
    clientWidth: 600,
    scrollLeft: 0,
    getBoundingClientRect: () => ({ left: 24 }),
    querySelector: () => ({
      getBoundingClientRect: () => ({ left: 24 + 1000 - strip.scrollLeft, width: 80 }),
    }),
  };
  const close = { addEventListener: (name, callback) => { documentHandlers[`close:${name}`] = callback; } };
  let previousClicks = 0;
  let nextClicks = 0;
  const previous = { click: () => { previousClicks += 1; } };
  const next = { click: () => { nextClicks += 1; } };
  const slides = [
    { dataset: { photoUrl: '/previous/' } },
    { dataset: { current: 'true', photoUrl: '/current/' } },
    { dataset: { photoUrl: '/next/' } },
  ];
  let focused = false;
  let blurred = false;
  const viewer = {
    addEventListener: (name, callback, options) => {
      documentHandlers[`viewer:${name}`] = callback;
      listenerOptions[name] = options;
    },
    focus: () => { focused = true; },
    blur: () => { blurred = true; focused = false; },
    querySelectorAll: () => slides,
    setAttribute() {},
  };
  const document = {
    querySelector: (selector) => ({
      '.photo-swiper': viewer,
      '.photo-close': close,
      '.photo-strip': strip,
      '[rel="prev"]': previous,
      '[rel="next"]': next,
    })[selector] ?? null,
    addEventListener: (name, callback) => { documentHandlers[name] = callback; },
    documentElement: {
      classList: { toggle: (name, enabled) => enabled ? documentClasses.add(name) : documentClasses.delete(name) },
    },
  };
  const location = { href: 'https://example.test/current/', hash: '' };
  const window = {
    addEventListener: (name, callback) => {
      const previous = windowHandlers[name];
      windowHandlers[name] = (event) => { previous?.(event); callback(event); };
    },
    dispatchEvent: (event) => windowHandlers[event.type]?.(event),
    location: { ...location, assign: (url) => visits.push(url) },
    matchMedia: () => ({ matches: false }),
    visualViewport: { scale: 1, addEventListener() {} },
    ResizeObserver: class { constructor(callback) { resizeObservers.push(callback); } observe() {} },
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
  return {
    documentClasses, documentHandlers, focused: () => focused, blurred: () => blurred, listenerOptions,
    nextClicks: () => nextClicks, previousClicks: () => previousClicks, resizeObservers,
    slides, strip, swiper: lastSwiper, visits, windowHandlers,
  };
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

test('double-clicking toggles image-only view without focusing the viewer', () => {
  const page = photoPage();
  page.swiper.emit('doubleTap');
  assert.equal(page.documentClasses.has('photo-only'), true);
  assert.equal(page.focused(), false);
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

function inputEvent(properties = {}) {
  return {
    prevented: false, stopped: false,
    preventDefault() { this.prevented = true; },
    stopImmediatePropagation() { this.stopped = true; },
    ...properties,
  };
}

test('Escape and Close reset zoom, exit image-only view, and remove image focus', () => {
  for (const action of ['escape', 'close']) {
    const page = photoPage();
    page.swiper.emit('doubleTap');
    page.swiper.zoom.in(3);
    page.swiper.animating = true;
    if (action === 'escape') page.documentHandlers.keydown(inputEvent({ key: 'Escape' }));
    else page.documentHandlers['close:click']();
    assert.equal(page.documentClasses.has('photo-only'), false);
    assert.equal(page.swiper.zoom.scale, 1);
    assert.equal(page.focused(), false);
    assert.equal(page.blurred(), true);
  }
});

test('Escape during committed navigation restores controls on the destination too', () => {
  const page = photoPage();
  page.swiper.emit('doubleTap');
  page.swiper.activeIndex = 2;
  page.swiper.emit('slideChangeTransitionStart');
  page.documentHandlers.keydown(inputEvent({ key: 'Escape' }));
  page.swiper.emit('slideChangeTransitionEnd');
  assert.deepEqual(page.visits, ['/next/']);
});

test('arrow keys activate the previous and next photo navigation links', () => {
  const page = photoPage();
  page.documentHandlers.keydown(inputEvent({ key: 'ArrowLeft' }));
  page.documentHandlers.keydown(inputEvent({ key: 'ArrowRight' }));
  assert.equal(page.previousClicks(), 1);
  assert.equal(page.nextClicks(), 1);
});

test('Control-wheel zoom is confined to the viewer, bounded, and reset by Escape', () => {
  const page = photoPage();
  const wheel = page.documentHandlers['viewer:wheel'];
  const zoom = inputEvent({ ctrlKey: true, deltaY: -50, deltaMode: 0 });
  wheel(zoom);
  assert.equal(zoom.prevented, true);
  assert.equal(zoom.stopped, true);
  assert.equal(page.listenerOptions.wheel.passive, false);
  assert.equal(page.listenerOptions.wheel.capture, true);
  assert.ok(page.swiper.zoom.scale > 1 && page.swiper.zoom.scale < 5);
  assert.equal(page.swiper.allowSlideNext, false);
  wheel(inputEvent({ ctrlKey: true, deltaY: -1000, deltaMode: 0 }));
  assert.equal(page.swiper.zoom.scale, 5);
  wheel(inputEvent({ ctrlKey: true, deltaY: 1000, deltaMode: 0 }));
  assert.equal(page.swiper.zoom.scale, 1);
  wheel(zoom);
  page.documentHandlers.keydown(inputEvent({ key: 'Escape' }));
  assert.equal(page.swiper.zoom.scale, 1);
  assert.equal(page.swiper.allowSlideNext, true);
  const scroll = inputEvent({ ctrlKey: false, deltaY: -50, deltaMode: 0 });
  wheel(scroll);
  assert.equal(scroll.prevented, false);
  assert.equal(scroll.stopped, false);
  assert.equal(page.swiper.zoom.scale, 1);
});

test('Safari gestures use relative image zoom and do not double-apply touchscreen pinches', () => {
  const page = photoPage();
  const start = inputEvent();
  page.documentHandlers['viewer:gesturestart'](start);
  page.documentHandlers['viewer:gesturechange'](inputEvent({ scale: 2 }));
  page.documentHandlers['viewer:gesturechange'](inputEvent({ scale: 3 }));
  assert.equal(start.prevented, true);
  assert.equal(page.swiper.zoom.scale, 3);
  page.documentHandlers['viewer:gestureend'](inputEvent());
  page.documentHandlers.keydown(inputEvent({ key: 'Escape' }));
  page.documentHandlers.pointerdown({ pointerId: 1 });
  page.documentHandlers.pointerdown({ pointerId: 2 });
  page.documentHandlers['viewer:gesturestart'](inputEvent());
  page.documentHandlers['viewer:gesturechange'](inputEvent({ scale: 2 }));
  assert.equal(page.swiper.zoom.scale, 1);
});

test('focused viewer keyboard zoom leaves browser modifier shortcuts alone', () => {
  const page = photoPage();
  const keydown = page.documentHandlers['viewer:keydown'];
  keydown(inputEvent({ key: '+' }));
  assert.equal(page.swiper.zoom.scale, 1.25);
  keydown(inputEvent({ key: '-' }));
  assert.equal(page.swiper.zoom.scale, 1);
  keydown(inputEvent({ key: '=' }));
  keydown(inputEvent({ key: '0' }));
  assert.equal(page.swiper.zoom.scale, 1);
  for (const modifier of ['ctrlKey', 'metaKey', 'altKey']) {
    const event = inputEvent({ key: '+', [modifier]: true });
    keydown(event);
    assert.equal(event.prevented, false);
    assert.equal(page.swiper.zoom.scale, 1);
  }
});

test('the current thumbnail is centered on load, resize, page restore, and exit', () => {
  const page = photoPage();
  assert.equal(page.strip.scrollLeft, 740);
  page.strip.clientWidth = 400;
  page.resizeObservers[0]();
  assert.equal(page.strip.scrollLeft, 840);
  page.strip.scrollLeft = 0;
  page.windowHandlers.pageshow({ persisted: true });
  assert.equal(page.strip.scrollLeft, 840);
  page.swiper.emit('doubleTap');
  page.strip.scrollLeft = 0;
  page.documentHandlers['close:click']();
  assert.equal(page.strip.scrollLeft, 840);
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
