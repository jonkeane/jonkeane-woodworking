// Project galleries use CSS and native image loading. Only the site's menu
// needs the theme's UI runtime; photo gestures have their own script.
import Alpine from 'jslibs/alpinejs/v3/alpinejs/dist/module.esm.js';
import collapse from 'jslibs/alpinejs/v3/collapse/dist/module.esm.js';

Alpine.plugin(collapse);
Alpine.start();
