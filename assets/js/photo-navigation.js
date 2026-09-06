// Keep gestures on the photo so the filmstrip and details can scroll normally.
const photo = document.querySelector('.flickr-photo .photo-full');

if (photo) {
  let start = null;
  let navigating = false;
  const wheelQuietPeriod = 75;
  let readyAt = Date.now() + wheelQuietPeriod;
  let wheelArmed = false;
  let horizontalDistance = 0;
  let lastWheelAt = Date.now();
  const navigate = (direction) => {
    const link = document.querySelector(`.photo-nav a[rel="${direction}"]`);
    if (link && !navigating) {
      navigating = true;
      window.location.assign(link.href);
    }
  };
  window.addEventListener('pageshow', () => {
    navigating = false;
    wheelArmed = false;
    horizontalDistance = 0;
    lastWheelAt = Date.now();
    readyAt = lastWheelAt + wheelQuietPeriod;
  });

  // Navigate immediately at the threshold. The quiet period only arms input
  // after loading/restoring a page, so leftover momentum cannot advance it.
  // Once navigation starts, navigate() rejects further attempts on this page.
  window.addEventListener('wheel', (event) => {
    const now = Date.now();
    const quiet = now - lastWheelAt >= wheelQuietPeriod;
    lastWheelAt = now;
    if (quiet) {
      horizontalDistance = 0;
      wheelArmed = now >= readyAt;
    }
    if (event.target !== photo || event.ctrlKey || !wheelArmed) {
      horizontalDistance = 0;
      return;
    }
    if (Math.abs(event.deltaX) <= Math.abs(event.deltaY)) {
      horizontalDistance = 0;
      return;
    }
    event.preventDefault();
    if (Math.sign(event.deltaX) !== Math.sign(horizontalDistance)) {
      horizontalDistance = 0;
    }
    const unit = event.deltaMode === 1 ? 16 : event.deltaMode === 2 ? photo.clientWidth : 1;
    horizontalDistance += event.deltaX * unit;
    if (Math.abs(horizontalDistance) >= 50) {
      wheelArmed = false;
      navigate(horizontalDistance > 0 ? 'next' : 'prev');
      horizontalDistance = 0;
    }
  }, { passive: false });

  photo.addEventListener('touchstart', (event) => {
    start = event.touches.length === 1
      ? { x: event.touches[0].clientX, y: event.touches[0].clientY }
      : null;
  }, { passive: true });

  photo.addEventListener('touchmove', (event) => {
    if (event.touches.length !== 1) start = null;
  }, { passive: true });

  photo.addEventListener('touchcancel', () => { start = null; }, { passive: true });

  photo.addEventListener('touchend', (event) => {
    const origin = start;
    start = null;
    if (!origin || event.touches.length || event.changedTouches.length !== 1) return;

    const dx = event.changedTouches[0].clientX - origin.x;
    const dy = event.changedTouches[0].clientY - origin.y;
    if (Math.abs(dx) < 50 || Math.abs(dx) <= Math.abs(dy)) return;

    const direction = dx < 0 ? 'next' : 'prev';
    navigate(direction);
  }, { passive: true });
}
