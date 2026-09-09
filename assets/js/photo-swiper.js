import Swiper from 'swiper';
import { Mousewheel, Zoom } from 'swiper/modules';

const viewer = document.querySelector('.photo-swiper');
const drawer = document.querySelector('.photo-drawer');
const details = drawer?.querySelector?.('details');
const summary = details?.querySelector?.('summary');
const strip = document.querySelector('.photo-strip');
const currentThumbnail = strip?.querySelector('[aria-current="page"]');

function centerThumbnail() {
  if (!currentThumbnail || !strip.clientWidth) return;
  const current = currentThumbnail.getBoundingClientRect();
  const bounds = strip.getBoundingClientRect();
  // Scroll only the strip; scrollIntoView can move the whole photo page.
  strip.scrollLeft += current.left + current.width / 2 - bounds.left - strip.clientWidth / 2;
}

if (currentThumbnail) {
  centerThumbnail();
  window.addEventListener('pageshow', centerThumbnail);
  window.addEventListener('resize', centerThumbnail);
  if (window.ResizeObserver) new window.ResizeObserver(centerThumbnail).observe(strip);
}

// Native <details> removes its contents immediately when closed. Keep it open
// until the drawer has slid down, then let its usual closed state take over.
if (drawer && details && summary) {
  let closing = false;
  summary.addEventListener('click', (event) => {
    if (!details.open || closing || window.matchMedia('(prefers-reduced-motion: reduce)').matches) return;

    event.preventDefault();
    closing = true;
    drawer.classList.add('is-closing');
    drawer.addEventListener('animationend', (animation) => {
      if (animation.animationName !== 'photo-details-down') return;
      drawer.classList.remove('is-closing');
      details.open = false;
      closing = false;
    }, { once: true });
  });
}

if (viewer) {
  const slides = Array.from(viewer.querySelectorAll('.swiper-slide'));
  const initialIndex = slides.findIndex((slide) => slide.dataset.current === 'true');
  const pointers = new Set();
  let gestureBlocked = false;
  let zoomScale = 1;
  let destination = null;
  let navigating = false;
  let resetting = false;
  let desktopGestureScale = null;
  const swiper = new Swiper(viewer, {
    init: false,
    modules: [Mousewheel, Zoom],
    initialSlide: initialIndex,
    slidesPerView: 1,
    slidesPerGroup: 1,
    loop: false,
    threshold: 10,
    simulateTouch: true,
    grabCursor: true,
    speed: window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 200,
    preventInteractionOnTransition: true,
    edgeSwipeDetection: true,
    mousewheel: {
      enabled: true,
      forceToAxis: true,
      releaseOnEdges: true,
      thresholdDelta: 10,
      thresholdTime: 500,
    },
    zoom: { maxRatio: 5, toggle: false },
  });

  function updatePhotoOnly() {
    const enabled = window.location.hash === '#photo-only';
    document.documentElement.classList.toggle('photo-only', enabled);
    viewer.setAttribute('aria-label', enabled
      ? 'Photo viewer. Pinch, Control-scroll, or use plus/minus to zoom. Press Escape or Close to reset zoom and restore controls.'
      : 'Photo viewer. Double-click or press Enter for image-only view. Pinch, Control-scroll, or use plus/minus to zoom; Escape resets zoom.');
    if (swiper.initialized) {
      resetZoom();
      swiper.update();
      updateNavigation();
    }
    centerThumbnail();
  }

  function setPhotoOnly(enabled, focusViewer = enabled) {
    // Exiting must work even while a slide is transitioning.
    if (enabled && (destination || swiper.animating)) return;
    const url = new URL(window.location.href);
    url.hash = enabled ? 'photo-only' : '';
    history.replaceState(history.state, '', url);
    window.dispatchEvent(new Event('hashchange'));
    // Keep keyboard controls available when opening the image-only view, but
    // don't leave an outline around the image after it is dismissed.
    if (enabled && focusViewer) viewer.focus({ preventScroll: true });
    else viewer.blur();
  }

  function resetZoom() {
    desktopGestureScale = null;
    swiper.zoom.out();
    gestureBlocked = pointers.size > 1;
    updateNavigation();
  }

  document.querySelector('.photo-close')?.addEventListener('click', () => setPhotoOnly(false));

  swiper.on('doubleTap', () => {
    // A pointer gesture should not add a keyboard focus outline to the image.
    setPhotoOnly(window.location.hash !== '#photo-only', false);
  });
  viewer.addEventListener('keydown', (event) => {
    if (event.ctrlKey || event.metaKey || event.altKey) return;
    if (event.key === 'Enter' || event.key === ' ') {
      event.preventDefault();
      setPhotoOnly(window.location.hash !== '#photo-only');
    } else if (['+', '=', '-', '0'].includes(event.key)) {
      event.preventDefault();
      if (event.key === '0') resetZoom();
      else zoomTo(zoomScale * (event.key === '-' ? 1 / 1.25 : 1.25));
    }
  });
  document.addEventListener('keydown', (event) => {
    if (event.key === 'ArrowLeft') {
      document.querySelector('[rel="prev"]')?.click();
    } else if (event.key === 'ArrowRight') {
      document.querySelector('[rel="next"]')?.click();
    } else if (event.key !== 'Escape') return;
    else if (window.location.hash === '#photo-only') {
      event.preventDefault();
      setPhotoOnly(false);
    } else if (zoomScale > 1) {
      event.preventDefault();
      resetZoom();
    }
  });
  window.addEventListener('hashchange', updatePhotoOnly);
  window.addEventListener('pageshow', updatePhotoOnly);

  function zoomTo(scale) {
    if (destination || swiper.animating) return;
    const next = Math.max(1, Math.min(5, scale));
    if (next === 1) swiper.zoom.out();
    else {
      // Seed the centered pan position: Swiper otherwise treats the first
      // numeric zoom as if a pointer were at the top-left of the page.
      if (zoomScale === 1) swiper.zoom.in(1);
      swiper.zoom.in(next);
    }
  }

  // Desktop trackpad pinches arrive as Ctrl-wheel in Chromium/Firefox.
  // Capture before Swiper's mousewheel navigation and cancel browser zoom.
  viewer.addEventListener('wheel', (event) => {
    if (!event.ctrlKey) return;
    event.preventDefault();
    event.stopImmediatePropagation();
    if (desktopGestureScale !== null) return;
    const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? viewer.clientHeight : 1;
    zoomTo(zoomScale * Math.exp(-event.deltaY * unit * 0.01));
  }, { capture: true, passive: false });

  // Safari uses GestureEvents for trackpad pinches. Touchscreen pinches
  // already belong to Swiper's two-pointer zoom handler.
  viewer.addEventListener('gesturestart', (event) => {
    event.preventDefault();
    desktopGestureScale = pointers.size > 1 ? null : zoomScale;
  }, { passive: false });
  viewer.addEventListener('gesturechange', (event) => {
    event.preventDefault();
    if (desktopGestureScale !== null) zoomTo(desktopGestureScale * event.scale);
  }, { passive: false });
  viewer.addEventListener('gestureend', (event) => {
    event.preventDefault();
    desktopGestureScale = null;
  }, { passive: false });

  function navigationAllowed() {
    return !gestureBlocked && zoomScale <= 1
      && (!window.visualViewport || window.visualViewport.scale <= 1);
  }

  function updateNavigation() {
    const allowed = !destination && !navigating && navigationAllowed();
    swiper.allowSlidePrev = allowed && initialIndex > 0;
    swiper.allowSlideNext = allowed && initialIndex < slides.length - 1;
  }

  function resetSlide() {
    resetting = true;
    swiper.allowSlidePrev = true;
    swiper.allowSlideNext = true;
    swiper.slideTo(initialIndex, 0, false);
    resetting = false;
    updateNavigation();
  }

  // This gates navigation while Swiper continues to own the gesture. Capture
  // runs before Swiper, including when a second finger lands during a drag.
  document.addEventListener('pointerdown', (event) => {
    if (pointers.size === 0) gestureBlocked = zoomScale > 1;
    pointers.add(event.pointerId);
    if (pointers.size > 1) {
      gestureBlocked = true;
      if (!destination) resetSlide();
    }
    updateNavigation();
  }, { capture: true, passive: true });

  function releasePointer(event) {
    if (event.type === 'pointercancel') gestureBlocked = true;
    pointers.delete(event.pointerId);
    // Keep the latch through Swiper's release handlers. Only a fresh gesture
    // may navigate after a pinch, even if it ended back at scale 1.
    updateNavigation();
  }

  document.addEventListener('pointerup', releasePointer, { capture: true, passive: true });
  document.addEventListener('pointercancel', releasePointer, { capture: true, passive: true });

  swiper.on('zoomChange', (_, scale) => {
    // The event precedes Swiper updating its own zoom.scale property.
    zoomScale = scale;
    if (pointers.size > 0 && scale > 1) gestureBlocked = true;
    updateNavigation();
  });
  swiper.on('update resize', updateNavigation);
  swiper.on('slideChangeTransitionStart', () => {
    if (resetting || destination || swiper.activeIndex === initialIndex) return;
    if (!navigationAllowed()) {
      resetSlide();
      return;
    }
    destination = slides[swiper.activeIndex].dataset.photoUrl;
    swiper.allowTouchMove = false;
    updateNavigation();
  });
  swiper.on('slideChangeTransitionEnd', () => {
    if (resetting || !destination || navigating) return;
    navigating = true;
    if (window.location.hash === '#photo-only') {
      const url = new URL(destination, window.location.href);
      url.hash = 'photo-only';
      window.location.assign(url.href);
    } else {
      window.location.assign(destination);
    }
  });

  window.visualViewport?.addEventListener('resize', updateNavigation);
  window.addEventListener('blur', () => {
    pointers.clear();
    gestureBlocked = true;
    updateNavigation();
  });
  window.addEventListener('pageshow', (event) => {
    if (!event.persisted) return;
    pointers.clear();
    gestureBlocked = false;
    destination = null;
    navigating = false;
    swiper.allowTouchMove = true;
    swiper.zoom.out();
    resetSlide();
  });

  updatePhotoOnly();
  swiper.init();
  updateNavigation();
}
