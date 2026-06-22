import { Star, StarOff } from 'lucide-react';
import type { AssetRating } from '../types/api';

const ratingValues: AssetRating[] = [0, 1, 2, 3, 4, 5];

export default function RatingStars({
  value,
  onChange,
}: {
  value: AssetRating;
  onChange: (value: AssetRating) => void;
}) {
  return (
    <div className="rating-stars" role="radiogroup" aria-label="星级">
      {ratingValues.map((rating) => (
        <button
          aria-checked={value === rating}
          aria-label={rating === 0 ? '未评级' : `${rating} 星`}
          className={rating === 0 ? ratingZeroClass(value) : ratingStarClass(value, rating)}
          key={rating}
          role="radio"
          title={rating === 0 ? '未评级' : `${rating} 星`}
          type="button"
          onClick={() => onChange(rating)}
        >
          {rating === 0 ? <StarOff size={15} /> : <Star size={15} fill={rating <= value ? 'currentColor' : 'none'} />}
        </button>
      ))}
    </div>
  );
}

function ratingZeroClass(value: AssetRating) {
  return value === 0 ? 'rating-star-button active zero' : 'rating-star-button zero';
}

function ratingStarClass(value: AssetRating, rating: AssetRating) {
  return rating <= value && value > 0 ? 'rating-star-button active' : 'rating-star-button';
}

export function ratingLabel(value: AssetRating) {
  return value === 0 ? '未评级' : `${value} 星`;
}

export function normalizeAssetRating(value: number): AssetRating {
  if (value === 1 || value === 2 || value === 3 || value === 4 || value === 5) {
    return value;
  }
  return 0;
}
