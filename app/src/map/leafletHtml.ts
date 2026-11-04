/**
 * Leaflet 地图页面的完整 HTML（作为字符串内联进 bundle）。
 *
 * 为什么不用 .html 文件：Metro 打包器不认 html 导入；字符串模块零配置可打包。
 * 与旧前端 useLeaflet 同源：WGS-84、瓦片源 tile.openstreetmap.de（大陆实测可达）。
 * RN → 地图的命令入口是 window.__handleCmd(jsonString)（见 MapWebView.injectJavaScript）。
 */
export const leafletHtml = `<!DOCTYPE html>
<html>
<head>
<meta charset="utf-8" />
<meta name="viewport" content="width=device-width, initial-scale=1, maximum-scale=1, user-scalable=no" />
<link rel="stylesheet" href="https://unpkg.com/leaflet@1.9.4/dist/leaflet.css" />
<style>
  html, body { margin: 0; padding: 0; height: 100%; background: #F5F5F7; }
  #map { width: 100%; height: 100%; }
  .leaflet-tooltip { font-size: 12px; }
</style>
</head>
<body>
<div id="map"></div>
<script src="https://unpkg.com/leaflet@1.9.4/dist/leaflet.js"><\/script>
<script>
  var DEFAULT_CENTER = [39.909, 116.397]; // 北京，纬度在前（Leaflet 顺序）
  var DEFAULT_ZOOM = 12;
  var MODE_COLORS = { driving: '#0066CC', walking: '#34A853', transit: '#E08600' };

  var map = L.map('map', { zoomControl: true, attributionControl: false })
    .setView(DEFAULT_CENTER, DEFAULT_ZOOM);
  L.tileLayer('https://tile.openstreetmap.de/{z}/{x}/{y}.png', { maxZoom: 18 }).addTo(map);

  var markers = {};   // id -> L.Marker
  var polylines = [];
  var ready = false;
  var pending = [];   // ready 前到达的命令先缓存

  function send(msg) {
    if (window.ReactNativeWebView) {
      window.ReactNativeWebView.postMessage(JSON.stringify(msg));
    }
  }

  map.on('click', function (e) {
    send({ type: 'mapTap', lat: e.latlng.lat, lng: e.latlng.lng });
  });

  function makeMarker(cmd) {
    var opts = { draggable: cmd.draggable !== false, title: cmd.title || '' };
    if (cmd.label) {
      // 编号圆点：与列表序号一一对应（label 由 RN 侧传 1 起始的序号）
      opts.icon = L.divIcon({
        className: 'rn-num-marker',
        html: '<div style="width:22px;height:22px;border-radius:11px;background:#1D1D1F;color:#fff;'
          + 'font:600 12px/22px -apple-system,sans-serif;text-align:center;box-shadow:0 1px 4px rgba(0,0,0,.35)">'
          + cmd.label + '</div>',
        iconSize: [22, 22],
        iconAnchor: [11, 11],
      });
    }
    var marker = L.marker([cmd.lat, cmd.lng], opts).addTo(map);
    marker.on('dragend', function () {
      var p = marker.getLatLng();
      send({ type: 'markerDrag', id: cmd.id, lat: p.lat, lng: p.lng });
    });
    if (cmd.title) {
      marker.bindTooltip(cmd.title, { permanent: !!cmd.label, direction: 'top', offset: [0, cmd.label ? -14 : -8] });
    }
    return marker;
  }

  function handle(cmd) {
    switch (cmd.type) {
      case 'addMarker':
        if (markers[cmd.id]) markers[cmd.id].setLatLng([cmd.lat, cmd.lng]);
        else markers[cmd.id] = makeMarker(cmd);
        break;
      case 'removeMarker':
        if (markers[cmd.id]) { map.removeLayer(markers[cmd.id]); delete markers[cmd.id]; }
        break;
      case 'clearMarkers':
        Object.keys(markers).forEach(function (id) { map.removeLayer(markers[id]); });
        markers = {};
        break;
      case 'drawPolyline':
        cmd.segments.forEach(function (seg) {
          var latlngs = seg.points.map(function (p) { return [p[1], p[0]]; }); // lng,lat -> lat,lng
          polylines.push(L.polyline(latlngs, {
            color: MODE_COLORS[seg.mode] || '#1D1D1F',
            weight: 4,
            opacity: 0.9,
          }).addTo(map));
        });
        break;
      case 'clearPolylines':
        polylines.forEach(function (l) { map.removeLayer(l); });
        polylines = [];
        break;
      case 'fitBounds': {
        var pts = [];
        Object.keys(markers).forEach(function (id) { pts.push(markers[id].getLatLng()); });
        polylines.forEach(function (l) {
          l.getLatLngs().forEach(function (ll) { pts.push(ll); });
        });
        if (pts.length === 1) map.setView(pts[0], Math.max(map.getZoom(), 14));
        else if (pts.length > 1) map.fitBounds(L.latLngBounds(pts).pad(0.15));
        break;
      }
      case 'setCenter':
        map.setView([cmd.lat, cmd.lng], cmd.zoom || map.getZoom());
        break;
      default:
        break;
    }
  }

  // RN 侧命令入口：injectJavaScript 执行 window.__handleCmd('<json>')
  window.__handleCmd = function (jsonStr) {
    var cmd;
    try { cmd = JSON.parse(jsonStr); } catch (err) { return; }
    if (!ready) { pending.push(cmd); return; }
    handle(cmd);
  };

  map.whenReady(function () {
    ready = true;
    pending.forEach(handle);
    pending = [];
    send({ type: 'ready' });
  });
<\/script>
</body>
</html>`;
