import wave
import pyaudio
from openai import OpenAI
from google_trans_new import google_translator

# Initialize OpenAI client with API key
client = OpenAI(api_key="your_openai_api_key")

# 🎙️ Step 1: Record German Speech
def record_audio(filename="recorded_audio.wav", duration=10, rate=44100):
    p = pyaudio.PyAudio()
    stream = p.open(format=pyaudio.paInt16, channels=1, rate=rate, input=True, frames_per_buffer=1024)
    
    print("🎤 Recording... Speak now!")
    frames = []
    
    for _ in range(0, int(rate / 1024 * duration)):
        data = stream.read(1024)
        frames.append(data)
    
    print("✅ Recording finished.")
    stream.stop_stream()
    stream.close()
    p.terminate()
    
    wf = wave.open(filename, "wb")
    wf.setnchannels(1)
    wf.setsampwidth(p.get_sample_size(pyaudio.paInt16))
    wf.setframerate(rate)
    wf.writeframes(b"".join(frames))
    wf.close()
    return filename

# 🎙️ Step 2: Transcribe German Audio to Text
def transcribe_audio(file_path):
    audio_file = open(file_path, "rb")
    transcription = client.audio.transcriptions.create(
        model="whisper-1",
        file=audio_file
    )
    return transcription.text

# 🌍 Step 3: Translate German Text to Turkish
def translate_to_turkish(german_text):
    translator = google_translator()
    translation = translator.translate(german_text, lang_src='de', lang_tgt='tr')
    return translation

# Run the process
if __name__ == "__main__":
    audio_file = record_audio()  # Record voice
    german_text = transcribe_audio(audio_file)  # Transcribe speech
    turkish_text = translate_to_turkish(german_text)  # Translate text
    
    print("\n📝 German Text:", german_text)
    print("🌍 Turkish Translation:" I sondern es kommt einfach jedes Jahr eine neue Version eben mal größer mal kleiner und so der aktuelle Unterbau auch das was ich heutigen Video zeigen werde was man als minimale Version annehmen sollte ist Eckmann script 2015 Ältere Versionen als das ist die nächst niedrigere Version wäre Eckmann Skript fünf damit sind wir so von der Zeit her ungefähr beim Jahrtausendwechsel, turkish_text)