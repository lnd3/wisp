// wisp dashboard hover layer. Tooltips enhance, never gate: every value
// is also on the page (direct labels, axes, the daily table). Text goes
// in via textContent only.
(function () {
  "use strict";
  var tip = document.getElementById("tip");
  if (!tip) return;
  var tipValue = tip.querySelector(".tip-value");
  var tipLabel = tip.querySelector(".tip-label");

  function show(el, x, y) {
    tipValue.textContent = el.dataset.value || "";
    tipLabel.textContent = el.dataset.label || "";
    tip.hidden = false;
    var r = tip.getBoundingClientRect();
    var left = Math.min(x + 12, window.innerWidth - r.width - 8);
    var top = y - r.height - 12;
    if (top < 8) top = y + 16;
    tip.style.left = Math.max(8, left) + "px";
    tip.style.top = top + "px";
  }
  function hide() { tip.hidden = true; }

  // Bars and columns: the mark (its whole row/slot) is the hit target.
  document.querySelectorAll(".bars li, .col").forEach(function (el) {
    el.addEventListener("pointermove", function (e) { show(el, e.clientX, e.clientY); });
    el.addEventListener("pointerleave", hide);
    el.addEventListener("focus", function () {
      var r = el.getBoundingClientRect();
      show(el, r.left + r.width / 2, r.top);
    });
    el.addEventListener("blur", hide);
  });

  // Line charts: a crosshair snaps to the nearest day.
  document.querySelectorAll(".plot").forEach(function (plot) {
    var cross = plot.querySelector(".crosshair");
    var dot = plot.querySelector(".hover-dot");
    plot.querySelectorAll(".hit").forEach(function (hit) {
      hit.addEventListener("pointermove", function (e) {
        cross.style.left = hit.dataset.x + "%";
        dot.style.left = hit.dataset.x + "%";
        dot.style.top = hit.dataset.y + "%";
        cross.hidden = dot.hidden = false;
        show(hit, e.clientX, e.clientY);
      });
    });
    plot.addEventListener("pointerleave", function () {
      cross.hidden = dot.hidden = true;
      hide();
    });
  });
})();
