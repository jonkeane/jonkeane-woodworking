import fjGallery from 'jslibs/flickr-justified-gallery/dist/fjGallery.esm.js';

// Use the theme's justified layout without its lightbox: photos remain ordinary
// links to their own pages, including when JavaScript is unavailable.
document.querySelectorAll('.flickr-grid').forEach((gallery) => {
  if (!gallery.querySelector('a')) return;

  fjGallery(gallery, {
    itemSelector: 'a',
    gutter: 10,
    lastRow: 'left',
    transitionDuration: false,
    onBeforeJustify() {
      gallery.classList.add('is-justified');
      // Smaller rows on phones, capped at 320px on wide screens.
      this.options.rowHeight = Math.min(320, Math.max(180, window.innerWidth * 0.21));
    },
  });
});
