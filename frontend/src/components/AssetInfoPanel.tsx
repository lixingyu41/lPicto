import type { Asset } from '../types/api';
import { formatBytes, formatDateTime, formatDuration } from '../utils/format';

interface Props {
  asset: Asset;
  title: string;
}

export default function AssetInfoPanel({ asset, title }: Props) {
  return (
    <div className="sidebar-control-stack">
      <div className="sidebar-control-title">{title}</div>
      <div className="sidebar-asset-info">
        <strong>{asset.filename}</strong>
        <span>{asset.relPath}</span>
        <dl>
          <div>
            <dt>类型</dt>
            <dd>{asset.mediaType === 'image' ? '照片' : '视频'}</dd>
          </div>
          <div>
            <dt>大小</dt>
            <dd>{formatBytes(asset.size)}</dd>
          </div>
          <div>
            <dt>时间</dt>
            <dd>{formatDateTime(asset.timelineAt)}</dd>
          </div>
          <div>
            <dt>星级</dt>
            <dd>{asset.rating === 0 ? '未评级' : `${asset.rating} 星`}</dd>
          </div>
          {asset.width && asset.height && (
            <div>
              <dt>尺寸</dt>
              <dd>
                {asset.width} x {asset.height}
              </dd>
            </div>
          )}
          {asset.duration !== null && (
            <div>
              <dt>时长</dt>
              <dd>{formatDuration(asset.duration)}</dd>
            </div>
          )}
          {asset.mediaType === 'video' && (
            <div>
              <dt>旋转</dt>
              <dd>{asset.rotation || 0}°</dd>
            </div>
          )}
        </dl>
      </div>
    </div>
  );
}
