import time
import cv2
import numpy as np
from concurrent.futures import ProcessPoolExecutor, ThreadPoolExecutor
import os
import logging

logging.basicConfig(
    level=logging.DEBUG,
    format="%(asctime)s - %(levelname)s - %(message)s"
)
logger = logging.getLogger(__name__)

# Config - please note that normaly I would move this to external file, but I want to submit only one file like you asked
# Harris
HARRIS_BLOCK_SIZE = 7
HARRIS_APERATURE_SIZE = 9
HARRIS_K = 0.05

# LK
BLUR_RESOLUTION = 0.5
LK_PARAMS = {
    'winSize': (15, 15),
    'maxLevel': 2,
    'criteria': (cv2.TERM_CRITERIA_EPS | cv2.TERM_CRITERIA_COUNT, 10, 0.03),
}

# RANSAC
RANSAC_THRESHHOLD = 1

# stitching
MINIMAL_PIXEL_COLUMN_INDEX = 10

# output
TARGET_FPS = 30
START_FRAME = 10

# Main
DYNAMIC_MOSAICS = ['Trees.mp4', 'Iguazu.mp4']
INPUT_DIR = "input"
OUTPUT_DIR = "my_output"
PROCESS_POOL_WORKERS = 4
THREAD_POOL_WORKERS = 2
# ----------------------------

# Constants
LEFT = "left"
RIGHT = "right"
UP = "up"
DOWN = "down"
DOCS_IMAGES_STRATEGY = "docs_images"
VIDEOS_STRATEGY = "videos"
# ----------------------------

def stabilize_horizontal_motion(matrix):
    # zero rotation components
    matrix[0, 1] = 0
    matrix[1, 0] = 0
    
    return matrix

def align_images(image1, image2, calc_direction = False):
    """
    Aligns image2 to image1 using the Lucas-Kanade optical flow method.
    
    Parameters:
        image1 (numpy.ndarray): First image (reference frame).
        image2 (numpy.ndarray): Second image (to be aligned).
    
    Returns:
        numpy.ndarray: Transformation matrix.
        numpy.ndarray: Aligned version of image2.
    """
    # Convert images to grayscale for LK
    gray1 = cv2.cvtColor(image1, cv2.COLOR_BGR2GRAY)
    gray2 = cv2.cvtColor(image2, cv2.COLOR_BGR2GRAY)
    
    # Detect good features to track in image1
    harris1 = cv2.cornerHarris(gray1, blockSize=HARRIS_BLOCK_SIZE, ksize=HARRIS_APERATURE_SIZE, k=HARRIS_K)
    harris1 = cv2.dilate(harris1, None)
    points1 = np.argwhere(harris1 > 0.01 * harris1.max())
    points1 = np.expand_dims(points1[:, [1, 0]], axis=1).astype(np.float32)
    
    
    def apply_blur(img):
        h, w = img.shape[:2]
        small_size = (max(1, int(w * BLUR_RESOLUTION)), max(1, int(h * BLUR_RESOLUTION)))
        blurred = cv2.resize(img, small_size)
        return cv2.resize(blurred, (w, h))
    
    # LK works better with blurred images (good thing I learned to Test 2) so lets blur :)
    gray1 = apply_blur(gray1)
    gray2 = apply_blur(gray2)
    
    # Calculate optical flow (Lucas-Kanade) to find corresponding points in image2
    points2, st, err = cv2.calcOpticalFlowPyrLK(gray1, gray2, points1, None, **LK_PARAMS)
    
    # Select valid points
    points1_valid = points1[st == 1]
    points2_valid = points2[st == 1]
    
    matrix, inliers = cv2.estimateAffinePartial2D(
        points1_valid, 
        points2_valid, 
        method=cv2.RANSAC, 
        ransacReprojThreshold=RANSAC_THRESHHOLD
        )
    
    matrix = to_homogeneous(matrix)
    
    direction = LEFT # default
    if calc_direction:
        direction = calc_motion_direction(points1_valid, points2_valid)

    return stabilize_horizontal_motion(matrix), direction

def calc_motion_direction(points1_valid, points2_valid):
    motion_vectors = points2_valid - points1_valid
    dx = motion_vectors[:, 0].mean()
    dy = motion_vectors[:, 1].mean()
    if abs(dx) > abs(dy):
        direction = RIGHT if dx > 0 else LEFT
    else:
        direction = DOWN if dy > 0 else UP
    return direction


def to_homogeneous(affine_matrix):
    """Converts a 3x2 affine matrix to a 3x3 homogeneous matrix."""
    return np.vstack([affine_matrix, [0, 0, 1]])

def extract_frames(video_path):
    """Extracts frames from a video."""
    cap = cv2.VideoCapture(video_path)
    frames = []
    while cap.isOpened():
        ret, frame = cap.read()
        if not ret:
            break
        frames.append(frame)
    cap.release()
    return frames

def calculate_transformations(frames):
    num_frames = len(frames)
    ref_index = num_frames // 2
    transformations = [np.eye(3)]  # Identity matrix for the reference frame

    y_translations = [0]

    # right transformations
    right_transform = np.eye(3)
    for i in range(ref_index + 1, num_frames):
        matrix, _ = align_images(frames[i - 1], frames[i])
        right_transform = right_transform @ matrix
        transformations.append(right_transform)
        y_translations.append(right_transform[1, 2])

    # left transformations
    left_transform = np.eye(3)
    for i in range(ref_index - 1, -1, -1):
        matrix, _ = align_images(frames[i + 1], frames[i])
        left_transform = matrix @ left_transform
        transformations.insert(0, left_transform)
        y_translations.insert(0, left_transform[1, 2])

    # Adjust all transformations to stabilize around the median Y translation
    median_y_translation = np.median(y_translations)
    for i in range(len(transformations)):
        transformations[i][1, 2] -= median_y_translation

    return transformations, ref_index

def calculate_canvas_size(frames, transformations, ref_index):
    """Calculates the final canvas size."""
    min_x, min_y = 0, 0
    max_x, max_y = frames[ref_index].shape[1], frames[ref_index].shape[0]

    for i in range(len(frames)):
        h, w = frames[i].shape[:2]
        corners = np.array([[0, 0, 1], [w, 0, 1], [w, h, 1], [0, h, 1]])
        
        transformed_corners = [np.dot(transformations[i], corner) for corner in corners]
        transformed_corners = np.array(transformed_corners)
        min_x = min(min_x, transformed_corners[:, 0].min())
        min_y = min(min_y, transformed_corners[:, 1].min())
        max_x = max(max_x, transformed_corners[:, 0].max())
        max_y = max(max_y, transformed_corners[:, 1].max())
        
    return int(max_x - min_x), int(max_y - min_y), -int(min_x), -int(min_y)

def trim_black_borders(image, threshold=10):
    """
    Trims nearly black rows and columns from the given image.
    
    Parameters:
        image (numpy.ndarray): Input image as a NumPy array.
        threshold (int): Threshold for detecting black pixels (default is 10).
        
    Returns:
        numpy.ndarray: Cropped image with black borders removed.
    """
    # Convert image to grayscale
    if len(image.shape) == 3:
        gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY)
    else:
        gray = image

    # Apply a threshold to detect "almost black" pixels
    _, binary = cv2.threshold(gray, threshold, 255, cv2.THRESH_BINARY)

    # Find non-black pixels
    non_black_pixels = np.where(binary > 0)

    if non_black_pixels[0].size == 0 or non_black_pixels[1].size == 0:
        # Return original image if no non-black pixels are found
        return image

    # Get bounding box of non-black pixels
    y_min, y_max = non_black_pixels[0].min(), non_black_pixels[0].max()
    x_min, x_max = non_black_pixels[1].min(), non_black_pixels[1].max()

    # Crop the image to the bounding box
    cropped_image = image[y_min:y_max + 1, x_min:x_max + 1]
    
    cv2.imwrite('cropped.jpg', cropped_image)

    return cropped_image



def stitch_panorama(video_name, warped_frames, canvas_size, frame_x_offset=0):
    """takes columns from frames into a panoramic mosaic."""
    canvas = np.zeros((canvas_size[1], canvas_size[0], 3), dtype=np.uint8)
    

    prev_leftmost_x = 0
    prev_warped_frame = []

    for i, warped_frame in enumerate(warped_frames):

        # Initialize curr_leftmost_x for the first frame
        curr_leftmost_x = 0

        # Find the leftmost non-black pixel in the current warped frame
        non_black_pixels = np.where(warped_frame.sum(axis=2) > 0)
        if non_black_pixels[1].size > 0:
            curr_leftmost_x = np.min(non_black_pixels[1])
            
        if len(prev_warped_frame) > 0:
            mask = (prev_warped_frame.sum(axis=2) > 0).astype(np.uint8)

            # Keep only the first x_movement columns in the mask
            # Keep only the columns between prev_leftmost_x and curr_leftmost_x
            mask[:, :prev_leftmost_x+frame_x_offset] = 0
            mask[:, curr_leftmost_x+frame_x_offset:] = 0
            
            # apply mask
            canvas[mask == 1] = prev_warped_frame[mask == 1]

        prev_leftmost_x = curr_leftmost_x
        prev_warped_frame = warped_frame
    
    logger.info('Stitched panorama for  offset %d for %s', frame_x_offset, video_name)
        
    return canvas

def images_to_video(images, output_path, fps=30):
    """
    Converts a list of images to an MP4 video.

    Parameters:
        images (list of numpy.ndarray): List of images (frames) to include in the video.
        output_path (str): Path to save the output video file.
        fps (int): Frames per second for the video.
    """
    if not images:
        raise ValueError("No images provided to create the video.")

    height, width, layers = images[0].shape

    fourcc = cv2.VideoWriter_fourcc(*'mp4v')
    out = cv2.VideoWriter(output_path, fourcc, fps, (width, height))

    for img in images:
        out.write(img)

    out.release()

def rotate_frame(frame, direction):
    """Rotates the frame based on the detected direction."""
    if direction == RIGHT:
        return cv2.rotate(frame, cv2.ROTATE_180)
    elif direction == LEFT:
        return frame  # No rotation needed
    elif direction == UP:
        return cv2.rotate(frame, cv2.ROTATE_90_CLOCKWISE)
    elif direction == DOWN:
        return cv2.rotate(frame, cv2.ROTATE_90_CLOCKWISE)

def rotate_frame_back(frame, direction):
    """Rotates the frame back to its original orientation."""
    if direction == RIGHT:
        return cv2.rotate(frame, cv2.ROTATE_180)
    elif direction == LEFT:
        return frame  # No rotation needed
    elif direction == UP:
        return cv2.rotate(frame, cv2.ROTATE_90_COUNTERCLOCKWISE)
    elif direction == DOWN:
        return cv2.rotate(frame, cv2.ROTATE_90_COUNTERCLOCKWISE)

def detect_motion_direction(frames):
    # vote with motion of first 5 frames relative to the first frame
    dirction_vote = {
        LEFT: 0,
        RIGHT: 0,
        UP: 0,
        DOWN: 0
    }
    
    for i in range(1,6):
        _, direction = align_images(frames[0], frames[i], calc_direction=True)
        dirction_vote[direction] += 1
        
    return max(dirction_vote, key=dirction_vote.get)

def generate_mosaic_video(video_path, output_dir, dynamic = False):
    
    video_dir, video_name = os.path.split(video_path)
    
    logger.info('Generating mosaic video for %s', video_name)
    if dynamic:
        logger.info('dynamic mosaic for: %s', video_path)
    start_time = time.time()
    
    # extract frames from the video
    frames = extract_frames(video_path)
    # remove black borders
    frames = [trim_black_borders(frame) for frame in frames]
    
    logger.info('Extracted %d frames from %s', len(frames), video_name)

    # detect motion
    motion_direction = detect_motion_direction(frames)
    logger.info(f"Detected motion direction for {video_name}: {motion_direction}")

    # Rotate frames to make the motion rightward
    frames = [rotate_frame(frame, motion_direction) for frame in frames]

    # Calculate transformations and stitch panorama
    logger.info('Calculating transformations for %s', video_name)
    transformations, ref_index = calculate_transformations(frames)
    logger.info('Calculated transformations DONE for %s', video_name)
    
    logger.info('Calculating canvas size for %s', video_name)
    canvas_size = calculate_canvas_size(frames, transformations, ref_index)
    logger.info('Calculated canvas size DONE for %s', video_name)
    
    offset_x, offset_y = canvas_size[2], canvas_size[3]
    def invert_transformation(matrix):
        transformation = np.linalg.inv(matrix)
        transformation[0, 2] += offset_x
        transformation[1, 2] += offset_y
        return transformation
    transformations = [invert_transformation(matrix) for matrix in transformations]
    
    warped_frames = [cv2.warpPerspective(frames[i], transformations[i], (canvas_size[0], canvas_size[1])) for i in range(len(frames))]
    
    # Stitch panorama
    desired_video_length = 1
    if dynamic:
        desired_video_length = 6
    total_frames = len(frames)
    step_size = max(total_frames // TARGET_FPS, 1) // desired_video_length
    if not dynamic:
        step_size*=2
    
    num_frames = (total_frames - START_FRAME) // step_size + 1

    # Generate evenly spaced indices between START_FRAME and end_frame
    selected_indices = np.linspace(MINIMAL_PIXEL_COLUMN_INDEX, total_frames, num_frames, dtype=int).tolist()
        
    logger.info('Stitching panorama for %s', video_name)
    
    
    with ThreadPoolExecutor(max_workers=THREAD_POOL_WORKERS) as executor:
        panoramas = list(executor.map(
            lambda i: stitch_panorama(video_name, warped_frames, canvas_size, i),
            selected_indices
        ))
        
    if dynamic:
        # trim black borders
        final_panoramas = [trim_black_borders(panorama) for panorama in panoramas]
        # reverse order
        final_panoramas = final_panoramas[::-1]
        
    else:
        panoramas_reverse = panoramas[::-1]
        final_panoramas = panoramas + panoramas_reverse
        
    final_panoramas = [rotate_frame_back(panorama, motion_direction) for panorama in final_panoramas]
        
    # Save the final video
    images_to_video(final_panoramas, f"{output_dir}/{video_name}", fps=TARGET_FPS)
    end_time = time.time()
    
    #log time in HH:MM:SS
    logger.info('Generated mosaic video for %s in %s', video_name, time.strftime("%H:%M:%S", time.gmtime(end_time - start_time)))

def process_video(input_path, output_path, is_dynamic):
    generate_mosaic_video(input_path, output_path, is_dynamic)

import matplotlib.pyplot as plt


def visualize_harris_corners(image, output_path="harris_corners.jpg"):
    """
    Detects and saves Harris corners on an image.
    :param image: Input image.
    :param output_path: Path to save the output image.
    """
    gray = cv2.cvtColor(image, cv2.COLOR_BGR2GRAY)
    harris_corners = cv2.cornerHarris(gray, blockSize=HARRIS_BLOCK_SIZE, ksize=HARRIS_APERATURE_SIZE, k=HARRIS_K)
    harris_corners = cv2.dilate(harris_corners, None)
    sanitized_corners = np.argwhere(harris_corners > 0.01 * harris_corners.max())
    sanitized_corners = np.expand_dims(sanitized_corners[:, [1, 0]], axis=1).astype(np.float32)

    img_with_corners = image.copy()
    img_with_corners[harris_corners > 0.01 * harris_corners.max()] = [0, 0, 255]

    cv2.imwrite(output_path, img_with_corners)
    print(f"Saved: {output_path}")
    return sanitized_corners

def visualize_optical_flow(image1, image2, image1_corners, max_points=50, output_path="optical_flow.jpg"):
    """
    Computes and saves optical flow between two images.
    :param image1: First image.
    :param image2: Second image.
    :param output_path: Path to save the output image.
    """
    gray1 = cv2.cvtColor(image1, cv2.COLOR_BGR2GRAY)
    gray2 = cv2.cvtColor(image2, cv2.COLOR_BGR2GRAY)

    # Calculate optical flow
    points2, st, err = cv2.calcOpticalFlowPyrLK(gray1, gray2, image1_corners, None, **LK_PARAMS)
    img_with_flow = image2.copy()
    valid_indices = np.where(st == 1)[0]
    np.random.shuffle(valid_indices)
    valid_indices = valid_indices[:max_points]

    for i in valid_indices:
        old = image1_corners[i].ravel()
        new = points2[i].ravel()
        x1, y1 = map(int, old)
        x2, y2 = map(int, new)
        img_with_flow = cv2.arrowedLine(img_with_flow, (x1, y1), (x2, y2), (0, 255, 0), 2, tipLength=0.5)

    cv2.imwrite(output_path, img_with_flow)
    print(f"Saved: {output_path}")

def generate_videos():
    processes = []

    videos = [video for video in os.listdir(INPUT_DIR) if video.endswith(".mp4")]
    
    with ProcessPoolExecutor(max_workers=PROCESS_POOL_WORKERS) as executor:
        futures = [
            executor.submit(
                process_video, 
                f"{INPUT_DIR}/{video}", 
                OUTPUT_DIR, 
                video in DYNAMIC_MOSAICS
            )
            for video in videos
        ]

    # Wait for all futures to complete
    for future in futures:
        future.result()


def docs_generation(video_filename):
    
    # extract frames from the video
    frames = extract_frames(f'{INPUT_DIR}/{video_filename}')
    frames = [trim_black_borders(frame) for frame in frames]
    corners = visualize_harris_corners(frames[0])
    
    visualize_optical_flow(frames[0], frames[30], corners)
    
    
    
if __name__ == "__main__":
    
    strategy = VIDEOS_STRATEGY
    
    if strategy == VIDEOS_STRATEGY:
        generate_videos()
    elif strategy == DOCS_IMAGES_STRATEGY:
        docs_generation('boat.mp4')