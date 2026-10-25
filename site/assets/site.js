/* Progressive enhancement for the AbiData site. Everything works without it. */
(function () {
  "use strict";
  var reduce = window.matchMedia("(prefers-reduced-motion: reduce)").matches;

  // Reveal sections as they scroll into view.
  var items = document.querySelectorAll(".reveal");
  if ("IntersectionObserver" in window && !reduce) {
    var io = new IntersectionObserver(function (entries) {
      entries.forEach(function (e) {
        if (e.isIntersecting) { e.target.classList.add("in"); io.unobserve(e.target); }
      });
    }, { rootMargin: "0px 0px -8% 0px" });
    items.forEach(function (el) { io.observe(el); });
  } else {
    items.forEach(function (el) { el.classList.add("in"); });
  }

  // Copy buttons on code blocks.
  document.querySelectorAll(".code").forEach(function (box) {
    var pre = box.querySelector("pre");
    if (!pre || !navigator.clipboard) return;
    var b = document.createElement("button");
    b.type = "button";
    b.className = "copy";
    b.textContent = "Copy";
    b.addEventListener("click", function () {
      navigator.clipboard.writeText(pre.innerText.replace(/\n$/, "")).then(function () {
        b.textContent = "Copied";
        setTimeout(function () { b.textContent = "Copy"; }, 1800);
      }, function () { b.textContent = "Press Ctrl+C"; });
    });
    box.appendChild(b);
  });

  // Screenshot carousel: tabs and arrows over the scroll-snap track.
  var car = document.querySelector("[data-carousel]");
  if (car) {
    var track = car.querySelector(".track");
    var slides = Array.prototype.slice.call(track.children);
    var tabs = Array.prototype.slice.call(car.querySelectorAll(".tabs button"));
    var current = 0;
    var go = function (i, focus) {
      i = (i + slides.length) % slides.length;
      var s = slides[i];
      track.scrollTo({ left: s.offsetLeft - track.offsetLeft - (track.clientWidth - s.clientWidth) / 2, behavior: reduce ? "auto" : "smooth" });
      select(i);
      if (focus && tabs[i]) tabs[i].focus();
    };
    var select = function (i) {
      current = i;
      tabs.forEach(function (t, j) { t.setAttribute("aria-selected", j === i ? "true" : "false"); t.tabIndex = j === i ? 0 : -1; });
      slides.forEach(function (s, j) { s.setAttribute("aria-hidden", "false"); s.classList.toggle("on", j === i); });
    };
    tabs.forEach(function (t, i) {
      t.addEventListener("click", function () { go(i); });
      t.addEventListener("keydown", function (e) {
        if (e.key === "ArrowRight") { e.preventDefault(); go(i + 1, true); }
        if (e.key === "ArrowLeft") { e.preventDefault(); go(i - 1, true); }
      });
    });
    var prev = car.querySelector("[data-prev]"), next = car.querySelector("[data-next]");
    if (prev) prev.addEventListener("click", function () { go(current - 1); });
    if (next) next.addEventListener("click", function () { go(current + 1); });
    if ("IntersectionObserver" in window) {
      var so = new IntersectionObserver(function (entries) {
        entries.forEach(function (e) {
          if (e.isIntersecting && e.intersectionRatio > 0.6) select(slides.indexOf(e.target));
        });
      }, { root: track, threshold: [0.6] });
      slides.forEach(function (s) { so.observe(s); });
    }
    select(0);
  }

  // Docs: start the page list collapsed on narrow screens.
  var nav = document.querySelector(".docs-nav-details");
  if (nav && window.matchMedia("(max-width: 760px)").matches) nav.removeAttribute("open");
})();
